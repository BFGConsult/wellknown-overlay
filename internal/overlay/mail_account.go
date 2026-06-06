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
	MailSetupPath                      = "/mail/setup"
	MailSetupImagePath                 = "/mail/setup-og.png"
	AppleMobileconfigPath              = "/.well-known/mail/apple.mobileconfig"
	AutodiscoverPath                   = "/Autodiscover/Autodiscover.xml"
	defaultProfileMatch                = "default"
)

func MailAccountPaths() []string {
	paths := ThunderbirdAutoconfigPaths()
	paths = append(paths, MailSetupPath)
	paths = append(paths, AppleMobileconfigPath)
	return append(paths, AutodiscoverPaths()...)
}

func (cfg MailAccount) Paths() []string {
	paths := MailAccountPaths()
	if cfg.SharePreviewEnabled() {
		paths = append(paths, MailSetupImagePath)
	}
	return paths
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
	return cfg.selectProfileCandidates(profileCandidates{emailAddresses: []string{normalizedEmail}})
}

func (cfg MailAccount) SelectProfileForRequest(emailAddress, host string) MailAccountProfile {
	normalizedEmail := strings.ToLower(strings.TrimSpace(emailAddress))
	if normalizedEmail != "" {
		return cfg.SelectProfile(normalizedEmail)
	}

	var candidates []string
	for _, domain := range mailDomainsFromHost(host) {
		candidates = append(candidates, "user@"+domain)
	}
	return cfg.selectProfileCandidates(profileCandidates{emailAddresses: candidates})
}

type profileCandidates struct {
	emailAddresses []string
}

func (cfg MailAccount) selectProfileCandidates(candidates profileCandidates) MailAccountProfile {
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
		if len(candidates.emailAddresses) == 0 {
			continue
		}
		matched := false
		for _, candidate := range candidates.emailAddresses {
			ok, err := path.Match(match, candidate)
			if err == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
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

func mailDomainsFromHost(host string) []string {
	host = normalizeRequestHost(host)
	if host == "" {
		return nil
	}
	candidates := []string{host}
	for _, prefix := range []string{"autoconfig.", "autodiscover."} {
		if strings.HasPrefix(host, prefix) {
			candidates = append(candidates, strings.TrimPrefix(host, prefix))
		}
	}
	return uniqueStrings(candidates)
}

func normalizeRequestHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}
	if first, _, ok := strings.Cut(host, ","); ok {
		host = strings.TrimSpace(first)
	}
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end >= 0 {
			return strings.Trim(host[:end+1], "[]")
		}
	}
	if withoutPort, _, ok := strings.Cut(host, ":"); ok {
		host = withoutPort
	}
	return strings.TrimSuffix(host, ".")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var unique []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func (cfg MailAccount) ManualSetupURL() string {
	if cfg.ManualSetup == nil {
		return ""
	}
	return cfg.ManualSetup.URL
}

func (cfg MailAccount) SharePreviewEnabled() bool {
	return cfg.ManualSetup != nil && cfg.ManualSetup.SharePreview != nil && cfg.ManualSetup.SharePreview.Enabled
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

func RenderThunderbirdAutoconfig(cfg MailAccountProfile, emailAddress, documentationURL string) ([]byte, error) {
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
			Documentation:    documentationXML(documentationURL),
		},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render email autoconfig: %w", err)
	}

	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func documentationXML(url string) *documentationURLXML {
	if url == "" {
		return nil
	}
	return &documentationURLXML{
		URL: url,
		Descriptions: []documentationDescriptionXML{
			{Lang: "en", Text: "Manual email setup instructions"},
		},
	}
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
	ID               string               `xml:"id,attr"`
	Domain           string               `xml:"domain"`
	DisplayName      string               `xml:"displayName"`
	DisplayShortName string               `xml:"displayShortName"`
	IncomingServer   serverXML            `xml:"incomingServer"`
	OutgoingServer   serverXML            `xml:"outgoingServer"`
	Documentation    *documentationURLXML `xml:"documentation,omitempty"`
}

type serverXML struct {
	Type           string `xml:"type,attr"`
	Hostname       string `xml:"hostname"`
	Port           int    `xml:"port"`
	SocketType     string `xml:"socketType"`
	Authentication string `xml:"authentication"`
	Username       string `xml:"username"`
}

type documentationURLXML struct {
	URL          string                        `xml:"url,attr"`
	Descriptions []documentationDescriptionXML `xml:"descr"`
}

type documentationDescriptionXML struct {
	Lang string `xml:"lang,attr"`
	Text string `xml:",chardata"`
}
