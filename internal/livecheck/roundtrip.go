package livecheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-msgauth/dkim"
)

type RoundTripOptions struct {
	Enabled          bool
	Password         string
	ReceiverEmail    string
	ReceiverPassword string
	SenderAutoconfig string
	DKIMSelectors    []string
	Timeout          time.Duration
	KeepMessage      bool
	InsecureTLS      bool
}

type roundTripMessage struct {
	ID        string
	Subject   string
	From      string
	To        string
	CreatedAt time.Time
	Body      []byte
}

type roundTripReceipt struct {
	FoundAfter time.Duration
	RawMessage []byte
	Deleted    bool
}

func (c Checker) CheckRoundTrip(ctx context.Context, email, domain string, opts RoundTripOptions) Result {
	result := Result{Name: "ROUND TRIP"}
	if strings.TrimSpace(opts.Password) == "" {
		result.Problems = append(result.Problems, "Round-trip test requires a password. Set LIVECHECK_PASSWORD or run interactively to enter it.")
		return result
	}
	receiverEmail := strings.TrimSpace(opts.ReceiverEmail)
	if receiverEmail == "" {
		receiverEmail = email
	}
	receiverPassword := opts.ReceiverPassword
	if receiverPassword == "" && strings.EqualFold(receiverEmail, email) {
		receiverPassword = opts.Password
	}
	if strings.TrimSpace(receiverPassword) == "" {
		result.Problems = append(result.Problems, "Round-trip receiver requires a password. Set LIVECHECK_RECEIVER_PASSWORD when LIVECHECK_RECEIVER_EMAIL is set.")
		return result
	}
	_, receiverDomain, err := parseEmail(receiverEmail)
	if err != nil {
		result.Problems = append(result.Problems, "invalid round-trip receiver email: "+err.Error())
		return result
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	senderSettings, err := c.discoverRoundTripSenderSettings(ctx, email, domain, opts.SenderAutoconfig)
	if err != nil {
		result.Problems = append(result.Problems, "could not derive sender IMAP/SMTP settings from Thunderbird Autoconfig: "+err.Error())
		return result
	}
	senderSettings = senderSettings.withEmailAddress(email)
	if err := validateRoundTripSenderSettings(senderSettings); err != nil {
		result.Problems = append(result.Problems, err.Error())
		return result
	}

	receiverSettings := senderSettings
	if !strings.EqualFold(receiverEmail, email) {
		receiverSettings, err = c.discoverMailSettings(ctx, receiverEmail, receiverDomain)
		if err != nil {
			result.Problems = append(result.Problems, "could not derive receiver IMAP settings from Thunderbird Autoconfig: "+err.Error())
			return result
		}
		receiverSettings = receiverSettings.withEmailAddress(receiverEmail)
	}
	if err := validateRoundTripReceiverSettings(receiverSettings); err != nil {
		result.Problems = append(result.Problems, err.Error())
		return result
	}

	result.Details = append(result.Details, fmt.Sprintf("SMTP %s:%d using %s as %s", senderSettings.Outgoing.Hostname, senderSettings.Outgoing.Port, displaySocketType(senderSettings.Outgoing.SocketType), senderSettings.Outgoing.Username))
	result.Details = append(result.Details, fmt.Sprintf("IMAP %s:%d using %s as %s", receiverSettings.Incoming.Hostname, receiverSettings.Incoming.Port, displaySocketType(receiverSettings.Incoming.SocketType), receiverSettings.Incoming.Username))
	if !strings.EqualFold(receiverEmail, email) {
		result.Details = append(result.Details, fmt.Sprintf("round-trip receiver: %s", receiverEmail))
	}

	message := c.newRoundTripMessage(email, receiverEmail)
	sendMail := c.SendMail
	if sendMail == nil {
		sendMail = sendRoundTripSMTP
	}
	pollIMAP := c.PollIMAP
	if pollIMAP == nil {
		pollIMAP = pollRoundTripIMAP
	}

	started := c.now()
	if err := sendMail(ctx, message, senderSettings.Outgoing, opts.Password, domain, opts.InsecureTLS); err != nil {
		result.Problems = append(result.Problems, "SMTP send failed: "+err.Error())
		return result
	}
	result.Details = append(result.Details, "SMTP sent test message")

	receipt, err := pollIMAP(ctx, message, receiverSettings.Incoming, receiverPassword, receiverDomain, opts.InsecureTLS, timeout, opts.KeepMessage)
	if err != nil {
		result.Problems = append(result.Problems, "IMAP receive failed: "+err.Error())
		return result
	}
	result.Passed = true
	if receipt.FoundAfter == 0 {
		receipt.FoundAfter = c.now().Sub(started)
	}
	result.Details = append(result.Details, fmt.Sprintf("IMAP found test message after %s", receipt.FoundAfter.Round(time.Second)))
	if receipt.Deleted {
		result.Details = append(result.Details, "test message deleted")
	} else if opts.KeepMessage {
		result.Details = append(result.Details, "test message kept")
	}

	dkimDetails, dkimProblems, dkimFatal := c.verifyRoundTripDKIM(receipt.RawMessage, domain, opts.DKIMSelectors)
	result.Details = append(result.Details, dkimDetails...)
	result.Problems = append(result.Problems, dkimProblems...)
	if dkimFatal {
		result.Passed = false
	}
	return result
}

func (c Checker) discoverRoundTripSenderSettings(ctx context.Context, email, domain, autoconfigPath string) (mailSettings, error) {
	if strings.TrimSpace(autoconfigPath) == "" {
		return c.discoverMailSettings(ctx, email, domain)
	}
	body, err := os.ReadFile(autoconfigPath)
	if err != nil {
		return mailSettings{}, err
	}
	return parseThunderbirdSettings(body, domain)
}

func (c Checker) newRoundTripMessage(from, to string) roundTripMessage {
	now := c.now()
	id := fmt.Sprintf("%d.%s", now.UnixNano(), sanitizeMessageIDLocal(from))
	messageID := fmt.Sprintf("<wellknown-overlay-livecheck.%s@%s>", id, domainFromEmail(from))
	subject := "wellknown-overlay livecheck " + id
	body := buildRoundTripMessage(messageID, subject, id, from, to, now)
	return roundTripMessage{
		ID:        id,
		Subject:   subject,
		From:      from,
		To:        to,
		CreatedAt: now,
		Body:      body,
	}
}

func buildRoundTripMessage(messageID, subject, id, from, to string, now time.Time) []byte {
	headers := textproto.MIMEHeader{}
	headers.Set("From", from)
	headers.Set("To", to)
	headers.Set("Subject", subject)
	headers.Set("Message-ID", messageID)
	headers.Set("Date", now.Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	headers.Set("Content-Type", `text/plain; charset="utf-8"`)
	headers.Set("X-Wellknown-Overlay-Livecheck-ID", id)

	var buf bytes.Buffer
	for _, key := range []string{"From", "To", "Subject", "Message-ID", "Date", "MIME-Version", "Content-Type", "X-Wellknown-Overlay-Livecheck-ID"} {
		fmt.Fprintf(&buf, "%s: %s\r\n", key, headers.Get(key))
	}
	fmt.Fprintf(&buf, "\r\nThis is an automated wellknown-overlay livecheck message.\r\nID: %s\r\n", id)
	return buf.Bytes()
}

func sendRoundTripSMTP(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
	host := server.Hostname
	addr := net.JoinHostPort(server.Hostname, fmt.Sprintf("%d", server.Port))
	tlsConfig := &tls.Config{ServerName: host, InsecureSkipVerify: insecureTLS}

	var smtpClient *smtp.Client
	var conn net.Conn
	var err error
	if isImplicitTLS(server.SocketType) {
		dialer := tls.Dialer{Config: tlsConfig}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			smtpClient, err = smtp.NewClient(conn, host)
			if err != nil {
				_ = conn.Close()
			}
		}
	} else {
		dialer := net.Dialer{}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			smtpClient, err = smtp.NewClient(conn, host)
			if err != nil {
				_ = conn.Close()
			}
		}
	}
	if err != nil {
		return err
	}
	defer smtpClient.Close()

	if err := smtpClient.Hello("wellknown-overlay-livecheck.localhost"); err != nil {
		return err
	}
	if isStartTLS(server.SocketType) {
		ok, _ := smtpClient.Extension("STARTTLS")
		if !ok {
			return errors.New("server does not advertise STARTTLS")
		}
		if err := smtpClient.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if err := smtpClient.Auth(smtp.PlainAuth("", server.Username, password, host)); err != nil {
		return err
	}
	if err := smtpClient.Mail(message.From); err != nil {
		return err
	}
	if err := smtpClient.Rcpt(message.To); err != nil {
		return err
	}
	w, err := smtpClient.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message.Body); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return smtpClient.Quit()
}

func pollRoundTripIMAP(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
	started := time.Now()
	deadline := started.Add(timeout)
	imapClient, err := dialIMAP(server, insecureTLS, timeout)
	if err != nil {
		return roundTripReceipt{}, err
	}
	defer imapClient.Logout()

	if err := imapClient.Login(server.Username, password); err != nil {
		return roundTripReceipt{}, err
	}
	if _, err := imapClient.Select("INBOX", false); err != nil {
		return roundTripReceipt{}, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return roundTripReceipt{}, err
		}
		raw, seqNum, err := findRoundTripMessage(imapClient, message)
		if err != nil {
			return roundTripReceipt{}, err
		}
		if seqNum != 0 {
			deleted := false
			if !keepMessage {
				if err := deleteIMAPMessage(imapClient, seqNum); err != nil {
					return roundTripReceipt{}, err
				}
				deleted = true
			}
			return roundTripReceipt{FoundAfter: time.Since(started), RawMessage: raw, Deleted: deleted}, nil
		}
		if time.Now().After(deadline) {
			return roundTripReceipt{}, fmt.Errorf("timed out after %s waiting for message %s", timeout, message.ID)
		}
		time.Sleep(3 * time.Second)
	}
}

func dialIMAP(server mailServerSettings, insecureTLS bool, timeout time.Duration) (*client.Client, error) {
	addr := net.JoinHostPort(server.Hostname, fmt.Sprintf("%d", server.Port))
	tlsConfig := &tls.Config{ServerName: server.Hostname, InsecureSkipVerify: insecureTLS}
	dialer := &net.Dialer{Timeout: minDuration(timeout, 15*time.Second)}
	if isImplicitTLS(server.SocketType) {
		return client.DialWithDialerTLS(dialer, addr, tlsConfig)
	}
	imapClient, err := client.DialWithDialer(dialer, addr)
	if err != nil {
		return nil, err
	}
	if isStartTLS(server.SocketType) {
		if err := imapClient.StartTLS(tlsConfig); err != nil {
			_ = imapClient.Logout()
			return nil, err
		}
	}
	return imapClient, nil
}

func findRoundTripMessage(imapClient *client.Client, message roundTripMessage) ([]byte, uint32, error) {
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("X-Wellknown-Overlay-Livecheck-ID", message.ID)
	seqNums, err := imapClient.Search(criteria)
	if err != nil {
		return nil, 0, err
	}
	if len(seqNums) == 0 {
		return nil, 0, nil
	}
	sort.Slice(seqNums, func(i, j int) bool { return seqNums[i] > seqNums[j] })

	section := &imap.BodySectionName{Peek: true}
	for _, seqNum := range seqNums {
		seqSet := new(imap.SeqSet)
		seqSet.AddNum(seqNum)
		messages := make(chan *imap.Message, 1)
		if err := imapClient.Fetch(seqSet, []imap.FetchItem{section.FetchItem()}, messages); err != nil {
			return nil, 0, err
		}
		msg := <-messages
		if msg == nil {
			continue
		}
		body := msg.GetBody(section)
		if body == nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(body, 2*1024*1024))
		if err != nil {
			return nil, 0, err
		}
		if bytes.Contains(raw, []byte("X-Wellknown-Overlay-Livecheck-ID: "+message.ID)) || bytes.Contains(raw, []byte(message.ID)) {
			return raw, seqNum, nil
		}
	}
	return nil, 0, nil
}

func deleteIMAPMessage(imapClient *client.Client, seqNum uint32) error {
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(seqNum)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := imapClient.Store(seqSet, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
		return err
	}
	return imapClient.Expunge(nil)
}

func (c Checker) verifyRoundTripDKIM(raw []byte, domain string, requiredSelectors []string) ([]string, []string, bool) {
	if len(raw) == 0 {
		return []string{"DKIM end-to-end verification: not checked; raw received message unavailable"}, nil, false
	}
	signatures := extractDKIMSignatureIdentities(raw)
	verifications, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{
		LookupTXT: func(name string) ([]string, error) {
			return c.lookupTXT(context.Background(), name)
		},
	})
	if err != nil {
		return []string{"DKIM end-to-end verification: failed"}, []string{"DKIM verification failed: " + err.Error()}, len(requiredSelectors) > 0
	}
	if len(verifications) == 0 {
		return []string{"DKIM end-to-end verification: unsigned message; no DKIM selector found"}, []string{"Received round-trip message has no DKIM signature, so no DKIM selector can be inferred from the round-trip."}, len(requiredSelectors) > 0
	}
	var details []string
	var problems []string
	var passingRequiredSelector bool
	for i, verification := range verifications {
		status := "pass"
		if verification.Err != nil {
			status = "fail: " + verification.Err.Error()
		}
		selector := ""
		if i < len(signatures) {
			selector = signatures[i].Selector
		}
		if selector == "" {
			details = append(details, fmt.Sprintf("DKIM signature d=%s: %s; selector unavailable", verification.Domain, status))
		} else {
			details = append(details, fmt.Sprintf("DKIM signature d=%s s=%s: %s", verification.Domain, selector, status))
		}
		if strings.EqualFold(verification.Domain, domain) && verification.Err != nil {
			problems = append(problems, "DKIM signature for "+domain+" failed: "+verification.Err.Error())
		}
		if strings.EqualFold(verification.Domain, domain) && verification.Err == nil && selectorInList(selector, requiredSelectors) {
			passingRequiredSelector = true
		}
	}
	if !hasPassingDKIMForDomain(verifications, domain) {
		problems = append(problems, "No passing DKIM signature for "+domain+" was found on the received message.")
	}
	if len(requiredSelectors) > 0 && !passingRequiredSelector {
		problems = append(problems, "No passing DKIM signature for "+domain+" used a configured selector. Expected one of: "+strings.Join(requiredSelectors, ", ")+".")
		return details, problems, true
	}
	return details, problems, false
}

type dkimSignatureIdentity struct {
	Domain   string
	Selector string
}

func extractDKIMSignatureIdentities(raw []byte) []dkimSignatureIdentity {
	headers := string(raw)
	if idx := strings.Index(headers, "\r\n\r\n"); idx >= 0 {
		headers = headers[:idx]
	} else if idx := strings.Index(headers, "\n\n"); idx >= 0 {
		headers = headers[:idx]
	}

	var signatures []dkimSignatureIdentity
	var currentName string
	var currentValue strings.Builder
	flush := func() {
		if !strings.EqualFold(currentName, "DKIM-Signature") {
			return
		}
		tags := parseMailAuthTags(currentValue.String())
		signatures = append(signatures, dkimSignatureIdentity{
			Domain:   tags["d"],
			Selector: tags["s"],
		})
	}

	for _, line := range strings.Split(strings.ReplaceAll(headers, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if currentName != "" {
				currentValue.WriteByte(' ')
				currentValue.WriteString(strings.TrimSpace(line))
			}
			continue
		}
		flush()
		currentName = ""
		currentValue.Reset()
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		currentName = strings.TrimSpace(name)
		currentValue.WriteString(strings.TrimSpace(value))
	}
	flush()
	return signatures
}

func hasPassingDKIMForDomain(verifications []*dkim.Verification, domain string) bool {
	for _, verification := range verifications {
		if strings.EqualFold(verification.Domain, domain) && verification.Err == nil {
			return true
		}
	}
	return false
}

func selectorInList(selector string, selectors []string) bool {
	if selector == "" {
		return false
	}
	for _, candidate := range selectors {
		if strings.EqualFold(selector, candidate) {
			return true
		}
	}
	return false
}

func validateRoundTripSenderSettings(settings mailSettings) error {
	if !strings.EqualFold(settings.Outgoing.Type, "smtp") {
		return fmt.Errorf("round-trip requires SMTP outgoing settings, got %q", settings.Outgoing.Type)
	}
	if settings.Outgoing.Username == "" {
		return errors.New("round-trip requires an outgoing username in the discovered sender settings")
	}
	if !supportsPasswordAuth(settings.Outgoing.Authentication) {
		return fmt.Errorf("round-trip currently supports password SMTP authentication only, got outgoing %q", settings.Outgoing.Authentication)
	}
	return nil
}

func validateRoundTripReceiverSettings(settings mailSettings) error {
	if !strings.EqualFold(settings.Incoming.Type, "imap") {
		return fmt.Errorf("round-trip requires IMAP incoming settings, got %q", settings.Incoming.Type)
	}
	if settings.Incoming.Username == "" {
		return errors.New("round-trip requires an incoming username in the discovered receiver settings")
	}
	if !supportsPasswordAuth(settings.Incoming.Authentication) {
		return fmt.Errorf("round-trip currently supports password IMAP authentication only, got incoming %q", settings.Incoming.Authentication)
	}
	return nil
}

func (settings mailSettings) withEmailAddress(email string) mailSettings {
	settings.Incoming.Username = replaceEmailPlaceholder(settings.Incoming.Username, email)
	settings.Outgoing.Username = replaceEmailPlaceholder(settings.Outgoing.Username, email)
	return settings
}

func replaceEmailPlaceholder(value, email string) string {
	local, _, ok := strings.Cut(email, "@")
	if !ok {
		local = email
	}
	value = strings.ReplaceAll(value, "%EMAILADDRESS%", email)
	value = strings.ReplaceAll(value, "%EMAILLOCALPART%", local)
	return value
}

func (c Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func isImplicitTLS(socketType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(socketType))
	return normalized == "ssl" || normalized == "tls" || normalized == "ssl/tls"
}

func isStartTLS(socketType string) bool {
	return strings.EqualFold(strings.TrimSpace(socketType), "starttls")
}

func displaySocketType(socketType string) string {
	if strings.TrimSpace(socketType) == "" {
		return "plain"
	}
	return socketType
}

func supportsPasswordAuth(authentication string) bool {
	normalized := strings.ToLower(strings.TrimSpace(authentication))
	return normalized == "" || normalized == "password-cleartext" || normalized == "plain"
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func sanitizeMessageIDLocal(email string) string {
	return strings.NewReplacer("@", ".", "+", ".", "_", ".", " ", ".").Replace(email)
}

func domainFromEmail(email string) string {
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return "localhost"
	}
	return domain
}
