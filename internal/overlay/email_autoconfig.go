package overlay

import (
	"encoding/xml"
	"fmt"
)

const (
	EmailAutoconfigWellKnownPath = "/.well-known/autoconfig/mail/config-v1.1.xml"
	EmailAutoconfigLegacyPath    = "/mail/config-v1.1.xml"
)

func EmailAutoconfigPaths() []string {
	return []string{
		EmailAutoconfigWellKnownPath,
		EmailAutoconfigLegacyPath,
	}
}

func RenderEmailAutoconfig(cfg EmailAutoconfig) ([]byte, error) {
	shortName := cfg.DisplayShortName
	if shortName == "" {
		shortName = cfg.DisplayName
	}

	doc := clientConfigXML{
		Version: "1.1",
		EmailProvider: emailProviderXML{
			ID:               cfg.Domain,
			Domain:           cfg.Domain,
			DisplayName:      cfg.DisplayName,
			DisplayShortName: shortName,
			IncomingServer:   serverConfigXML(cfg.Incoming),
			OutgoingServer:   serverConfigXML(cfg.Outgoing),
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render email autoconfig: %w", err)
	}

	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func serverConfigXML(cfg EmailServerConfig) serverXML {
	return serverXML{
		Type:           cfg.Type,
		Hostname:       cfg.Hostname,
		Port:           cfg.Port,
		SocketType:     cfg.SocketType,
		Authentication: cfg.Authentication,
		Username:       cfg.Username,
	}
}

type clientConfigXML struct {
	XMLName       xml.Name         `xml:"clientConfig"`
	Version       string           `xml:"version,attr"`
	EmailProvider emailProviderXML `xml:"emailProvider"`
}

type emailProviderXML struct {
	ID               string    `xml:"id,attr"`
	Domain           string    `xml:"domain"`
	DisplayName      string    `xml:"displayName"`
	DisplayShortName string    `xml:"displayShortName"`
	IncomingServer   serverXML `xml:"incomingServer"`
	OutgoingServer   serverXML `xml:"outgoingServer"`
}

type serverXML struct {
	Type           string `xml:"type,attr"`
	Hostname       string `xml:"hostname"`
	Port           int    `xml:"port"`
	SocketType     string `xml:"socketType"`
	Authentication string `xml:"authentication"`
	Username       string `xml:"username"`
}
