package livecheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Profile string

const (
	Thunderbird Profile = "THUNDERBIRD"
	Outlook     Profile = "OUTLOOK"
	Apple       Profile = "APPLE"
	All         Profile = "ALL"
)

type Options struct {
	EmailAddress    string
	Profiles        []Profile
	InsecureTLS     bool
	Verbose         bool
	SkipMailAuthDNS bool
	Timeout         time.Duration
}

type Checker struct {
	HTTPClient *http.Client
	LookupHost func(context.Context, string) ([]string, error)
	LookupSRV  func(context.Context, string, string, string) (string, []*net.SRV, error)
	LookupTXT  func(context.Context, string) ([]string, error)
	Stdout     io.Writer
}

type Result struct {
	Name     string
	Passed   bool
	Details  []string
	Problems []string
}

func ParseProfiles(args []string) ([]Profile, error) {
	if len(args) == 0 {
		return []Profile{Thunderbird, Outlook, Apple}, nil
	}

	seen := make(map[Profile]struct{})
	var profiles []Profile
	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			value := Profile(strings.ToUpper(strings.TrimSpace(part)))
			if value == "" {
				continue
			}
			if value == All {
				return []Profile{Thunderbird, Outlook, Apple}, nil
			}
			switch value {
			case Thunderbird, Outlook, Apple:
				if _, ok := seen[value]; ok {
					continue
				}
				seen[value] = struct{}{}
				profiles = append(profiles, value)
			default:
				return nil, fmt.Errorf("unknown profile %q", part)
			}
		}
	}
	if len(profiles) == 0 {
		return nil, errors.New("no profiles selected")
	}
	return profiles, nil
}

func Run(ctx context.Context, opts Options, stdout io.Writer) (bool, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	client := newHTTPClient(timeout, opts.InsecureTLS)
	checker := Checker{
		HTTPClient: client,
		LookupHost: func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		},
		LookupSRV: func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
			return net.DefaultResolver.LookupSRV(ctx, service, proto, name)
		},
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			return net.DefaultResolver.LookupTXT(ctx, name)
		},
		Stdout: stdout,
	}
	return runWithChecker(ctx, opts, stdout, checker)
}

func runWithChecker(ctx context.Context, opts Options, stdout io.Writer, checker Checker) (bool, error) {
	email, domain, err := parseEmail(opts.EmailAddress)
	if err != nil {
		return false, err
	}
	profiles := opts.Profiles
	if len(profiles) == 0 {
		profiles = []Profile{Thunderbird, Outlook, Apple}
	}

	fmt.Fprintf(stdout, "Livecheck for %s\n", email)
	fmt.Fprintf(stdout, "Domain: %s\n", domain)
	if opts.InsecureTLS {
		fmt.Fprintln(stdout, "TLS certificate verification: disabled for diagnosis")
	} else {
		fmt.Fprintln(stdout, "TLS certificate verification: enabled")
	}

	allPassed := true
	for _, profile := range profiles {
		var result Result
		switch profile {
		case Thunderbird:
			result = checker.CheckThunderbird(ctx, email, domain)
		case Outlook:
			result = checker.CheckOutlook(ctx, email, domain)
		case Apple:
			result = checker.CheckApple(ctx, email, domain)
		default:
			return false, fmt.Errorf("unsupported profile %q", profile)
		}
		writeResult(stdout, result, opts.Verbose)
		if !result.Passed {
			allPassed = false
		}
	}
	writeResult(stdout, checker.CheckDNSSRV(ctx, email, domain), true)
	advisoryWarnings := false
	if !opts.SkipMailAuthDNS {
		mailAuthResult := checker.CheckMailAuthDNS(ctx, domain)
		writeResult(stdout, mailAuthResult, true)
		advisoryWarnings = len(mailAuthResult.Problems) > 0
	}

	if allPassed {
		if advisoryWarnings {
			fmt.Fprintln(stdout, "\nSummary: PASS (with warnings)")
		} else {
			fmt.Fprintln(stdout, "\nSummary: PASS")
		}
	} else {
		fmt.Fprintln(stdout, "\nSummary: FAIL")
	}
	return allPassed, nil
}

func (c Checker) CheckThunderbird(ctx context.Context, email, domain string) Result {
	result := Result{Name: string(Thunderbird)}
	autoconfigHost := "autoconfig." + domain
	result.Details = append(result.Details, c.describeDNS(ctx, autoconfigHost)...)

	var attempts []fetchAttempt
	var attemptProblems []string
	urls := []string{
		withEmailQuery("https://"+autoconfigHost+"/mail/config-v1.1.xml", email),
		withEmailQuery("https://"+domain+"/.well-known/autoconfig/mail/config-v1.1.xml", email),
	}
	for _, target := range urls {
		attempt := c.fetch(ctx, http.MethodGet, target, "", nil)
		attempts = append(attempts, attempt)
		result.Details = append(result.Details, attempt.details...)
		if attempt.err != nil {
			attemptProblems = append(attemptProblems, attempt.problem("Thunderbird discovery failed"))
			continue
		}
		if err := validateThunderbird(attempt.body, domain); err != nil {
			attemptProblems = append(attemptProblems, fmt.Sprintf("%s returned invalid Thunderbird XML: %v", target, err))
			attemptProblems = append(attemptProblems, "Suggestion: check that this standards path reaches the overlay and not the main backend application.")
			continue
		}
		result.Passed = true
		result.Details = append(result.Details, "valid Thunderbird autoconfig XML")
	}
	if !result.Passed {
		result.Problems = append(result.Problems, attemptProblems...)
		result.Problems = append(result.Problems, "Thunderbird requires at least one discovery path to work.")
		result.Problems = append(result.Problems, "Suggestion: configure DNS and HTTPS routing for autoconfig."+domain+" or route /.well-known/autoconfig/ on "+domain+" to the overlay.")
	} else if hasFailedAttempt(attempts) {
		result.Details = append(result.Details, "one or more optional Thunderbird discovery paths failed; rerun with -v for details")
	}
	return result
}

func (c Checker) CheckOutlook(ctx context.Context, email, domain string) Result {
	result := Result{Name: string(Outlook)}
	autodiscoverHost := "autodiscover." + domain
	result.Details = append(result.Details, c.describeDNS(ctx, autodiscoverHost)...)

	requestBody := autodiscoverRequest(email)
	var attempts []fetchAttempt
	var attemptProblems []string
	urls := []string{
		"https://" + domain + "/Autodiscover/Autodiscover.xml",
		"https://" + autodiscoverHost + "/Autodiscover/Autodiscover.xml",
	}
	for _, target := range urls {
		attempt := c.fetch(ctx, http.MethodPost, target, "text/xml; charset=utf-8", strings.NewReader(requestBody))
		attempts = append(attempts, attempt)
		result.Details = append(result.Details, attempt.details...)
		if attempt.err != nil {
			attemptProblems = append(attemptProblems, attempt.problem("Outlook Autodiscover failed"))
			continue
		}
		if err := validateAutodiscover(attempt.body); err != nil {
			attemptProblems = append(attemptProblems, fmt.Sprintf("%s returned invalid Autodiscover XML: %v", target, err))
			attemptProblems = append(attemptProblems, "Suggestion: check that the Autodiscover path is routed to the overlay and returns mail protocol settings.")
			continue
		}
		result.Passed = true
		result.Details = append(result.Details, "valid Outlook Autodiscover XML")
	}
	if !result.Passed {
		result.Problems = append(result.Problems, attemptProblems...)
		result.Problems = append(result.Problems, "Outlook requires at least one Autodiscover endpoint to work.")
		result.Problems = append(result.Problems, "Suggestion: configure DNS and HTTPS routing for autodiscover."+domain+" or route /Autodiscover/Autodiscover.xml on "+domain+" to the overlay.")
	} else if hasFailedAttempt(attempts) {
		result.Details = append(result.Details, "one or more optional Outlook Autodiscover endpoints failed; rerun with -v for details")
	}
	return result
}

func (c Checker) CheckApple(ctx context.Context, email, domain string) Result {
	result := Result{Name: string(Apple)}
	target := withEmailQuery("https://"+domain+"/.well-known/mail/apple.mobileconfig", email)
	attempt := c.fetch(ctx, http.MethodGet, target, "", nil)
	result.Details = append(result.Details, attempt.details...)
	if attempt.err != nil {
		result.Problems = append(result.Problems, attempt.problem("Apple mobileconfig failed"))
		result.Problems = append(result.Problems, "Suggestion: route /.well-known/mail/apple.mobileconfig on "+domain+" to the overlay and ensure the TLS certificate is valid for "+domain+".")
		return result
	}
	if err := validateAppleMobileconfig(attempt.body, email); err != nil {
		result.Problems = append(result.Problems, fmt.Sprintf("%s returned invalid Apple mobileconfig: %v", target, err))
		result.Problems = append(result.Problems, "Suggestion: check that this standards path reaches the overlay and returns an XML configuration profile.")
		return result
	}
	result.Passed = true
	result.Details = append(result.Details, "valid Apple mobileconfig")
	return result
}

func (c Checker) CheckDNSSRV(ctx context.Context, email, domain string) Result {
	result := Result{Name: "DNS SRV", Passed: true}

	settings, err := c.discoverMailSettings(ctx, email, domain)
	if err != nil {
		result.Details = append(result.Details, "could not derive expected IMAP/SMTP SRV records from Thunderbird Autoconfig: "+err.Error())
		result.Details = append(result.Details, "Suggestion: rerun after Thunderbird Autoconfig is reachable, or verify RFC 6186 SRV records manually.")
		return result
	}

	expected := []expectedSRV{
		{
			Service: "_autodiscover._tcp",
			Name:    "autodiscover",
			Proto:   "tcp",
			Domain:  domain,
			Target:  "autodiscover." + domain + ".",
			Port:    443,
			Weight:  0,
			Comment: "Outlook Autodiscover service discovery",
		},
	}

	switch strings.ToLower(settings.Incoming.Type) {
	case "imap":
		if strings.EqualFold(settings.Incoming.SocketType, "SSL") || strings.EqualFold(settings.Incoming.SocketType, "TLS") || strings.EqualFold(settings.Incoming.SocketType, "SSL/TLS") {
			expected = append(expected, expectedSRV{
				Service: "_imaps._tcp",
				Name:    "imaps",
				Proto:   "tcp",
				Domain:  domain,
				Target:  dnsTarget(settings.Incoming.Hostname),
				Port:    settings.Incoming.Port,
				Weight:  1,
				Comment: "RFC 6186 implicit-TLS IMAP discovery",
			})
		} else {
			expected = append(expected, expectedSRV{
				Service: "_imap._tcp",
				Name:    "imap",
				Proto:   "tcp",
				Domain:  domain,
				Target:  dnsTarget(settings.Incoming.Hostname),
				Port:    settings.Incoming.Port,
				Weight:  1,
				Comment: "RFC 6186 IMAP discovery",
			})
		}
	case "pop", "pop3":
		service := "pop3"
		label := "_pop3._tcp"
		comment := "RFC 6186 POP3 discovery"
		if strings.EqualFold(settings.Incoming.SocketType, "SSL") || strings.EqualFold(settings.Incoming.SocketType, "TLS") || strings.EqualFold(settings.Incoming.SocketType, "SSL/TLS") {
			service = "pop3s"
			label = "_pop3s._tcp"
			comment = "RFC 6186 implicit-TLS POP3 discovery"
		}
		expected = append(expected, expectedSRV{
			Service: label,
			Name:    service,
			Proto:   "tcp",
			Domain:  domain,
			Target:  dnsTarget(settings.Incoming.Hostname),
			Port:    settings.Incoming.Port,
			Weight:  1,
			Comment: comment,
		})
	}

	if strings.EqualFold(settings.Outgoing.Type, "smtp") {
		expected = append(expected, expectedSRV{
			Service: "_submission._tcp",
			Name:    "submission",
			Proto:   "tcp",
			Domain:  domain,
			Target:  dnsTarget(settings.Outgoing.Hostname),
			Port:    settings.Outgoing.Port,
			Weight:  1,
			Comment: "RFC 6186 message submission discovery",
		})
	}

	var missing []expectedSRV
	for _, want := range expected {
		detail, ok := c.checkSRV(ctx, want)
		result.Details = append(result.Details, detail)
		if !ok {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		result.Details = append(result.Details, "suggested DNS records:")
		for _, want := range missing {
			result.Details = append(result.Details, fmt.Sprintf("%s.%s. 3600 IN SRV 0 %d %d %s", want.Service, domain, want.Weight, want.Port, want.Target))
		}
		result.Details = append(result.Details, "optional explicit not-offered records when POP/plain IMAP are intentionally unsupported:")
		result.Details = append(result.Details, fmt.Sprintf("_imap._tcp.%s. 3600 IN SRV 0 0 0 .", domain))
		result.Details = append(result.Details, fmt.Sprintf("_pop3._tcp.%s. 3600 IN SRV 0 0 0 .", domain))
		result.Details = append(result.Details, fmt.Sprintf("_pop3s._tcp.%s. 3600 IN SRV 0 0 0 .", domain))
	}
	return result
}

func (c Checker) discoverMailSettings(ctx context.Context, email, domain string) (mailSettings, error) {
	autoconfigHost := "autoconfig." + domain
	urls := []string{
		withEmailQuery("https://"+autoconfigHost+"/mail/config-v1.1.xml", email),
		withEmailQuery("https://"+domain+"/.well-known/autoconfig/mail/config-v1.1.xml", email),
	}
	var lastErr error
	for _, target := range urls {
		attempt := c.fetch(ctx, http.MethodGet, target, "", nil)
		if attempt.err != nil {
			lastErr = attempt.err
			continue
		}
		settings, err := parseThunderbirdSettings(attempt.body, domain)
		if err != nil {
			lastErr = err
			continue
		}
		return settings, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no Thunderbird Autoconfig URL was attempted")
	}
	return mailSettings{}, lastErr
}

func (c Checker) checkSRV(ctx context.Context, want expectedSRV) (string, bool) {
	_, records, err := c.lookupSRV(ctx, want.Name, want.Proto, want.Domain)
	if err != nil {
		return fmt.Sprintf("%s.%s: missing (%s)", want.Service, want.Domain, want.Comment), false
	}
	for _, record := range records {
		target := dnsTarget(record.Target)
		if record.Port == want.Port && strings.EqualFold(target, want.Target) {
			return fmt.Sprintf("%s.%s: %d %d %d %s", want.Service, want.Domain, record.Priority, record.Weight, record.Port, dnsTarget(record.Target)), true
		}
	}
	var got []string
	for _, record := range records {
		got = append(got, fmt.Sprintf("%d %d %d %s", record.Priority, record.Weight, record.Port, dnsTarget(record.Target)))
	}
	sort.Strings(got)
	return fmt.Sprintf("%s.%s: unexpected value %s; expected port %d target %s", want.Service, want.Domain, strings.Join(got, "; "), want.Port, want.Target), false
}

func (c Checker) lookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	if c.LookupSRV != nil {
		return c.LookupSRV(ctx, service, proto, name)
	}
	return net.DefaultResolver.LookupSRV(ctx, service, proto, name)
}

func (c Checker) lookupTXT(ctx context.Context, name string) ([]string, error) {
	if c.LookupTXT != nil {
		return c.LookupTXT(ctx, name)
	}
	return net.DefaultResolver.LookupTXT(ctx, name)
}

type expectedSRV struct {
	Service string
	Name    string
	Proto   string
	Domain  string
	Target  string
	Port    uint16
	Weight  uint16
	Comment string
}

type mailSettings struct {
	Incoming mailServerSettings
	Outgoing mailServerSettings
}

type mailServerSettings struct {
	Type       string
	Hostname   string
	Port       uint16
	SocketType string
}

func (c Checker) describeDNS(ctx context.Context, host string) []string {
	addrs, err := c.lookup(ctx, host)
	if err != nil {
		return []string{
			fmt.Sprintf("DNS %s: FAIL: %v", host, err),
			"Suggestion: add DNS for " + host + " pointing at the host or load balancer serving the overlay.",
		}
	}
	sort.Strings(addrs)
	return []string{fmt.Sprintf("DNS %s: %s", host, strings.Join(addrs, ", "))}
}

func (c Checker) lookup(ctx context.Context, host string) ([]string, error) {
	if c.LookupHost != nil {
		return c.LookupHost(ctx, host)
	}
	return net.DefaultResolver.LookupHost(ctx, host)
}

type fetchAttempt struct {
	url       string
	status    int
	body      []byte
	details   []string
	err       error
	tlsFailed bool
}

func (c Checker) fetch(ctx context.Context, method, target, contentType string, body io.Reader) fetchAttempt {
	attempt := fetchAttempt{url: target}
	var redirects []string
	baseClient := c.HTTPClient
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	client := *baseClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		redirects = append(redirects, req.URL.String())
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		attempt.err = err
		return attempt
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", "wellknown-overlay-livecheck")

	resp, err := client.Do(req)
	if err != nil {
		attempt.err = err
		attempt.tlsFailed = isTLSError(err)
		attempt.details = append(attempt.details, fmt.Sprintf("%s %s: FAIL: %v", method, target, err))
		return attempt
	}
	defer resp.Body.Close()

	attempt.status = resp.StatusCode
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		attempt.err = err
		return attempt
	}
	attempt.body = responseBody
	attempt.details = append(attempt.details, fmt.Sprintf("%s %s: HTTP %d, content-type %q", method, target, resp.StatusCode, resp.Header.Get("Content-Type")))
	if len(redirects) > 0 {
		attempt.details = append(attempt.details, "redirects: "+target+" -> "+strings.Join(redirects, " -> "))
	}
	if resp.StatusCode != http.StatusOK {
		attempt.err = fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return attempt
}

func (a fetchAttempt) problem(prefix string) string {
	if a.tlsFailed {
		return prefix + " for " + a.url + ": TLS verification failed. Suggestion: ensure the certificate is valid for this hostname, or rerun with --insecure only for diagnosis."
	}
	return prefix + " for " + a.url + ": " + a.err.Error()
}

func validateThunderbird(body []byte, expectedDomain string) error {
	_, err := parseThunderbirdSettings(body, expectedDomain)
	return err
}

func parseThunderbirdSettings(body []byte, expectedDomain string) (mailSettings, error) {
	var settings mailSettings
	foundDomain := false
	incoming := false
	outgoing := false
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return mailSettings{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "domain":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return mailSettings{}, err
			}
			if strings.EqualFold(strings.TrimSpace(value), expectedDomain) {
				foundDomain = true
			}
		case "incomingServer":
			server, err := decodeThunderbirdServer(decoder, start)
			if err != nil {
				return mailSettings{}, err
			}
			settings.Incoming = server
			incoming = true
		case "outgoingServer":
			server, err := decodeThunderbirdServer(decoder, start)
			if err != nil {
				return mailSettings{}, err
			}
			settings.Outgoing = server
			outgoing = true
		}
	}
	if !foundDomain {
		return mailSettings{}, fmt.Errorf("expected domain %q not found", expectedDomain)
	}
	if !incoming || !outgoing {
		return mailSettings{}, errors.New("incoming and outgoing server sections are required")
	}
	if settings.Incoming.Hostname == "" || settings.Incoming.Port == 0 || settings.Outgoing.Hostname == "" || settings.Outgoing.Port == 0 {
		return mailSettings{}, errors.New("incoming and outgoing hostnames and ports are required")
	}
	return settings, nil
}

func decodeThunderbirdServer(decoder *xml.Decoder, start xml.StartElement) (mailServerSettings, error) {
	var server mailServerSettings
	for _, attr := range start.Attr {
		if attr.Name.Local == "type" {
			server.Type = strings.TrimSpace(attr.Value)
		}
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return server, nil
		}
		if err != nil {
			return server, err
		}
		switch token := token.(type) {
		case xml.EndElement:
			if token.Name.Local == start.Name.Local {
				return server, nil
			}
		case xml.StartElement:
			var value string
			if err := decoder.DecodeElement(&value, &token); err != nil {
				return server, err
			}
			switch token.Name.Local {
			case "hostname":
				server.Hostname = strings.TrimSpace(value)
			case "port":
				var port uint64
				if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &port); err != nil {
					return server, fmt.Errorf("invalid port %q", value)
				}
				if port > 65535 {
					return server, fmt.Errorf("invalid port %q", value)
				}
				server.Port = uint16(port)
			case "socketType":
				server.SocketType = strings.TrimSpace(value)
			}
		}
	}
}

func validateAutodiscover(body []byte) error {
	hasAccount := false
	hasIMAPOrPOP := false
	hasSMTP := false
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "Account":
			hasAccount = true
		case "Type":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return err
			}
			switch strings.ToUpper(strings.TrimSpace(value)) {
			case "IMAP", "POP3":
				hasIMAPOrPOP = true
			case "SMTP":
				hasSMTP = true
			}
		}
	}
	if !hasAccount {
		return errors.New("Account section is required")
	}
	if !hasIMAPOrPOP || !hasSMTP {
		return errors.New("IMAP/POP and SMTP protocol sections are required")
	}
	return nil
}

func validateAppleMobileconfig(body []byte, email string) error {
	hasMailPayload := bytes.Contains(body, []byte("com.apple.mail.managed"))
	hasEmail := bytes.Contains(body, []byte(email))
	if err := validateWellFormedXML(body); err != nil {
		return err
	}
	if !hasMailPayload {
		return errors.New("com.apple.mail.managed payload is required")
	}
	if !hasEmail {
		return fmt.Errorf("email address %q not found", email)
	}
	return nil
}

func validateWellFormedXML(body []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		if _, err := decoder.Token(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func writeResult(w io.Writer, result Result, verbose bool) {
	status := "FAIL"
	if result.Passed {
		status = "PASS"
	}
	fmt.Fprintf(w, "\n[%s] %s\n", result.Name, status)
	for _, detail := range result.Details {
		if !verbose && result.Passed && isVerboseOnlyDetail(detail) {
			continue
		}
		fmt.Fprintf(w, "  - %s\n", detail)
	}
	for _, problem := range result.Problems {
		fmt.Fprintf(w, "  ! %s\n", problem)
	}
}

func hasFailedAttempt(attempts []fetchAttempt) bool {
	for _, attempt := range attempts {
		if attempt.err != nil {
			return true
		}
	}
	return false
}

func isVerboseOnlyDetail(detail string) bool {
	return strings.Contains(detail, ": FAIL:") ||
		strings.HasPrefix(detail, "Suggestion: add DNS for ")
}

func parseEmail(value string) (string, string, error) {
	email := strings.TrimSpace(value)
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", "", fmt.Errorf("invalid email address %q", value)
	}
	return email, strings.ToLower(domain), nil
}

func withEmailQuery(rawURL, email string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("emailaddress", email)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func dnsTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return value
	}
	return strings.TrimRight(value, ".") + "."
}

func autodiscoverRequest(email string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/requestschema/2006">
  <Request>
    <EMailAddress>` + xmlEscape(email) + `</EMailAddress>
    <AcceptableResponseSchema>http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a</AcceptableResponseSchema>
  </Request>
</Autodiscover>`
}

func xmlEscape(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func newHTTPClient(timeout time.Duration, insecureTLS bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func isTLSError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return true
	}
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "certificate")
}
