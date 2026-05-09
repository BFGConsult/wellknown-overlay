package overlay

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	autodiscoverResponseNamespace = "http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006"
	autodiscoverOutlookNamespace  = "http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a"
)

func RenderAutodiscover(cfg MailAccountProfile, emailAddress string) ([]byte, error) {
	cfg.Incoming.Username = substituteMailVariables(cfg.Incoming.Username, emailAddress)
	cfg.Outgoing.Username = substituteMailVariables(cfg.Outgoing.Username, emailAddress)

	doc := autodiscoverXML{
		Response: autodiscoverResponseXML{
			User: autodiscoverUserXML{
				DisplayName:  cfg.DisplayName,
				EmailAddress: emailAddress,
			},
			Account: autodiscoverAccountXML{
				AccountType: "email",
				Action:      "settings",
				Protocols: []autodiscoverProtocolXML{
					autodiscoverProtocol(cfg.Incoming, false),
					autodiscoverProtocol(cfg.Outgoing, true),
				},
			},
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render autodiscover: %w", err)
	}

	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func EmailAddressFromAutodiscoverRequest(body []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "EMailAddress") {
			continue
		}

		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
}

func autodiscoverProtocol(cfg EmailServerConfig, outgoing bool) autodiscoverProtocolXML {
	protocol := autodiscoverProtocolXML{
		Type:           autodiscoverProtocolType(cfg.Type, outgoing),
		Server:         cfg.Hostname,
		Port:           cfg.Port,
		DomainRequired: "off",
		LoginName:      cfg.Username,
		SPA:            autodiscoverSPA(cfg.Authentication),
		AuthRequired:   autodiscoverAuthRequired(cfg.Authentication),
	}

	switch strings.ToLower(cfg.SocketType) {
	case "ssl", "tls", "ssl/tls":
		protocol.SSL = "on"
	case "starttls":
		protocol.Encryption = "TLS"
	default:
		if !outgoing {
			protocol.SSL = "off"
		}
	}

	return protocol
}

func autodiscoverProtocolType(value string, outgoing bool) string {
	if outgoing {
		return "SMTP"
	}
	switch strings.ToLower(value) {
	case "pop", "pop3":
		return "POP3"
	default:
		return "IMAP"
	}
}

func autodiscoverAuthRequired(value string) string {
	if strings.EqualFold(value, "none") {
		return "off"
	}
	return "on"
}

func autodiscoverSPA(value string) string {
	switch strings.ToLower(value) {
	case "ntlm", "gssapi", "kerberos":
		return "on"
	default:
		return "off"
	}
}

type autodiscoverXML struct {
	XMLName  xml.Name                `xml:"http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006 Autodiscover"`
	Response autodiscoverResponseXML `xml:"http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a Response"`
}

type autodiscoverResponseXML struct {
	User    autodiscoverUserXML    `xml:"User"`
	Account autodiscoverAccountXML `xml:"Account"`
}

type autodiscoverUserXML struct {
	DisplayName  string `xml:"DisplayName"`
	EmailAddress string `xml:"EmailAddress,omitempty"`
}

type autodiscoverAccountXML struct {
	AccountType string                    `xml:"AccountType"`
	Action      string                    `xml:"Action"`
	Protocols   []autodiscoverProtocolXML `xml:"Protocol"`
}

type autodiscoverProtocolXML struct {
	Type           string `xml:"Type"`
	Server         string `xml:"Server"`
	Port           int    `xml:"Port"`
	DomainRequired string `xml:"DomainRequired"`
	LoginName      string `xml:"LoginName"`
	SPA            string `xml:"SPA"`
	SSL            string `xml:"SSL,omitempty"`
	Encryption     string `xml:"Encryption,omitempty"`
	AuthRequired   string `xml:"AuthRequired"`
}
