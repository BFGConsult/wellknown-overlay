package overlay

import (
	"encoding/xml"
	"fmt"
	"path"
	"strings"
)

const (
	ThunderbirdAutoconfigWellKnownPath = "/.well-known/autoconfig/mail/config-v1.1.xml"
	ThunderbirdAutoconfigLegacyPath    = "/mail/config-v1.1.xml"
	AppleMobileconfigPath              = "/.well-known/mail/apple.mobileconfig"
	AutodiscoverPath                   = "/Autodiscover/Autodiscover.xml"
	defaultProfileMatch                = "default"
)

func MailAccountPaths() []string {
	paths := ThunderbirdAutoconfigPaths()
	paths = append(paths, AppleMobileconfigPath)
	return append(paths, AutodiscoverPaths()...)
}

func ThunderbirdAutoconfigPaths() []string {
	return []string{
		ThunderbirdAutoconfigWellKnownPath,
		ThunderbirdAutoconfigLegacyPath,
	}
}

func AutodiscoverPaths() []string {
	return []string{
		AutodiscoverPath,
		"/AutoDiscover/AutoDiscover.xml",
		"/autodiscover/autodiscover.xml",
	}
}

func (cfg MailAccount) SelectProfile(emailAddress string) MailAccountProfile {
	normalizedEmail := strings.ToLower(emailAddress)
	var defaultProfile MailAccountProfile
	var bestProfile MailAccountProfile
	var bestScore profileMatchScore
	hasBest := false

	for index, profile := range cfg.Profiles {
		match := strings.ToLower(profile.Match)
		if match == defaultProfileMatch {
			defaultProfile = profile
			continue
		}
		if normalizedEmail == "" {
			continue
		}
		matched, err := path.Match(match, normalizedEmail)
		if err != nil || !matched {
			continue
		}

		score := newProfileMatchScore(match, index)
		if !hasBest || score.betterThan(bestScore) {
			bestProfile = profile
			bestScore = score
			hasBest = true
		}
	}

	if hasBest {
		return bestProfile
	}
	return defaultProfile
}

type profileMatchScore struct {
	literalCount  int
	wildcardCount int
	index         int
}

func newProfileMatchScore(pattern string, index int) profileMatchScore {
	score := profileMatchScore{index: index}
	for _, ch := range pattern {
		switch ch {
		case '*', '?':
			score.wildcardCount++
		default:
			score.literalCount++
		}
	}
	return score
}

func (score profileMatchScore) betterThan(other profileMatchScore) bool {
	if score.literalCount != other.literalCount {
		return score.literalCount > other.literalCount
	}
	if score.wildcardCount != other.wildcardCount {
		return score.wildcardCount < other.wildcardCount
	}
	return score.index < other.index
}

func RenderThunderbirdAutoconfig(cfg MailAccountProfile, emailAddress string) ([]byte, error) {
	cfg.Incoming.Username = substituteMailVariables(cfg.Incoming.Username, emailAddress)
	cfg.Outgoing.Username = substituteMailVariables(cfg.Outgoing.Username, emailAddress)

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
