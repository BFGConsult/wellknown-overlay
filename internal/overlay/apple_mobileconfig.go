package overlay

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"strings"
)

func RenderAppleMobileconfig(cfg MailAccountProfile, emailAddress string) ([]byte, error) {
	username := substituteMailVariables(cfg.Incoming.Username, emailAddress)
	outgoingUsername := substituteMailVariables(cfg.Outgoing.Username, emailAddress)

	profileID := "org.wellknown-overlay." + cfg.Domain + ".mail"
	mailPayloadID := profileID + ".account"

	profile := plistDict{
		{Key: "PayloadContent", Value: plistArray{plistDict{
			{Key: "PayloadType", Value: plistString("com.apple.mail.managed")},
			{Key: "PayloadVersion", Value: plistInteger(1)},
			{Key: "PayloadIdentifier", Value: plistString(mailPayloadID)},
			{Key: "PayloadUUID", Value: plistString(stableUUID(mailPayloadID))},
			{Key: "PayloadDisplayName", Value: plistString(cfg.DisplayName)},
			{Key: "EmailAccountDescription", Value: plistString(cfg.DisplayName)},
			{Key: "EmailAccountName", Value: plistString(cfg.DisplayName)},
			{Key: "EmailAccountType", Value: plistString(appleAccountType(cfg.Incoming.Type))},
			{Key: "IncomingMailServerAuthentication", Value: plistString(appleAuthentication(cfg.Incoming.Authentication))},
			{Key: "IncomingMailServerHostName", Value: plistString(cfg.Incoming.Hostname)},
			{Key: "IncomingMailServerPortNumber", Value: plistInteger(cfg.Incoming.Port)},
			{Key: "IncomingMailServerUseSSL", Value: plistBool(appleUseSSL(cfg.Incoming.SocketType))},
			{Key: "IncomingMailServerUsername", Value: plistString(username)},
			{Key: "OutgoingMailServerAuthentication", Value: plistString(appleAuthentication(cfg.Outgoing.Authentication))},
			{Key: "OutgoingMailServerHostName", Value: plistString(cfg.Outgoing.Hostname)},
			{Key: "OutgoingMailServerPortNumber", Value: plistInteger(cfg.Outgoing.Port)},
			{Key: "OutgoingMailServerUseSSL", Value: plistBool(appleUseSSL(cfg.Outgoing.SocketType))},
			{Key: "OutgoingMailServerUsername", Value: plistString(outgoingUsername)},
			{Key: "OutgoingPasswordSameAsIncomingPassword", Value: plistBool(true)},
		}}},
		{Key: "PayloadType", Value: plistString("Configuration")},
		{Key: "PayloadVersion", Value: plistInteger(1)},
		{Key: "PayloadIdentifier", Value: plistString(profileID)},
		{Key: "PayloadUUID", Value: plistString(stableUUID(profileID))},
		{Key: "PayloadDisplayName", Value: plistString(cfg.DisplayName)},
		{Key: "PayloadDescription", Value: plistString("Configures mail for " + cfg.DisplayName + ".")},
		{Key: "PayloadOrganization", Value: plistString(cfg.DisplayName)},
	}

	if emailAddress != "" {
		mailPayload := profile[0].Value.(plistArray)[0].(plistDict)
		mailPayload = append(mailPayload[:7], append(plistDict{{Key: "EmailAddress", Value: plistString(emailAddress)}}, mailPayload[7:]...)...)
		profile[0].Value = plistArray{mailPayload}
	}

	var body bytes.Buffer
	body.WriteString(xml.Header)
	body.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	body.WriteString(`<plist version="1.0">` + "\n")
	if err := profile.writePlist(&body, ""); err != nil {
		return nil, err
	}
	body.WriteString(`</plist>` + "\n")
	return body.Bytes(), nil
}

func substituteMailVariables(value, emailAddress string) string {
	if emailAddress == "" {
		return value
	}
	localPart := emailAddress
	if before, _, ok := strings.Cut(emailAddress, "@"); ok {
		localPart = before
	}
	value = strings.ReplaceAll(value, "%EMAILADDRESS%", emailAddress)
	value = strings.ReplaceAll(value, "%EMAILLOCALPART%", localPart)
	return value
}

func appleAccountType(value string) string {
	switch strings.ToLower(value) {
	case "pop", "pop3":
		return "EmailTypePOP"
	default:
		return "EmailTypeIMAP"
	}
}

func appleAuthentication(value string) string {
	switch strings.ToLower(value) {
	case "none":
		return "EmailAuthNone"
	case "md5", "cram-md5":
		return "EmailAuthCRAMMD5"
	case "ntlm":
		return "EmailAuthNTLM"
	case "http-md5":
		return "EmailAuthHTTPMD5"
	default:
		return "EmailAuthPassword"
	}
}

func appleUseSSL(value string) bool {
	switch strings.ToLower(value) {
	case "none", "plain", "plaintext":
		return false
	default:
		return true
	}
}

func stableUUID(value string) string {
	sum := sha1.Sum([]byte(value))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}

type plistValue interface {
	writePlist(*bytes.Buffer, string) error
}

type plistEntry struct {
	Key   string
	Value plistValue
}

type plistDict []plistEntry
type plistArray []plistValue
type plistString string
type plistInteger int
type plistBool bool

func (d plistDict) writePlist(w *bytes.Buffer, indent string) error {
	w.WriteString(indent + "<dict>\n")
	for _, entry := range d {
		w.WriteString(indent + "  <key>")
		if err := xml.EscapeText(w, []byte(entry.Key)); err != nil {
			return err
		}
		w.WriteString("</key>\n")
		if err := entry.Value.writePlist(w, indent+"  "); err != nil {
			return err
		}
	}
	w.WriteString(indent + "</dict>\n")
	return nil
}

func (a plistArray) writePlist(w *bytes.Buffer, indent string) error {
	w.WriteString(indent + "<array>\n")
	for _, value := range a {
		if err := value.writePlist(w, indent+"  "); err != nil {
			return err
		}
	}
	w.WriteString(indent + "</array>\n")
	return nil
}

func (s plistString) writePlist(w *bytes.Buffer, indent string) error {
	w.WriteString(indent + "<string>")
	if err := xml.EscapeText(w, []byte(s)); err != nil {
		return err
	}
	w.WriteString("</string>\n")
	return nil
}

func (i plistInteger) writePlist(w *bytes.Buffer, indent string) error {
	w.WriteString(fmt.Sprintf("%s<integer>%d</integer>\n", indent, i))
	return nil
}

func (b plistBool) writePlist(w *bytes.Buffer, indent string) error {
	if b {
		w.WriteString(indent + "<true/>\n")
		return nil
	}
	w.WriteString(indent + "<false/>\n")
	return nil
}
