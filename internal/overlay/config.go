package overlay

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
)

const (
	RouteTargetBackend     = "backend"
	RouteTargetOverlayOnly = "overlay_only"
	RouteSourceBackend     = "backend"
)

type Config struct {
	Routes      []Route      `json:"routes,omitempty"`
	MailAccount *MailAccount `json:"mail_account,omitempty"`
}

type Route struct {
	Path          string `json:"path"`
	File          string `json:"file,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	Target        string `json:"target,omitempty"`
	Source        string `json:"source,omitempty"`
	SourceHost    string `json:"source_host,omitempty"`
	Status        int    `json:"status,omitempty"`
	CacheStatuses []int  `json:"cache_statuses,omitempty"`
}

type MailAccount struct {
	ManualSetup *MailManualSetupConfig `json:"manual_setup,omitempty"`
	Profiles    []MailAccountProfile   `json:"profiles"`
}

type MailManualSetupConfig struct {
	URL           string                   `json:"url,omitempty"`
	SharePreview  *MailSharePreviewConfig  `json:"share_preview,omitempty"`
	ExtraSections []MailManualSetupSection `json:"extra_sections,omitempty"`
}

type MailSharePreviewConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

type MailManualSetupSection struct {
	Lang         string `json:"lang,omitempty"`
	Title        string `json:"title"`
	BodyMarkdown string `json:"body_markdown"`
}

type MailAccountProfile struct {
	Match            string            `json:"match"`
	Domain           string            `json:"domain"`
	DisplayName      string            `json:"display_name"`
	DisplayShortName string            `json:"display_short_name,omitempty"`
	Incoming         EmailServerConfig `json:"incoming"`
	Outgoing         EmailServerConfig `json:"outgoing"`
}

type EmailServerConfig struct {
	Type           string `json:"type"`
	Hostname       string `json:"hostname"`
	Port           int    `json:"port"`
	SocketType     string `json:"socket_type"`
	Authentication string `json:"authentication"`
	Username       string `json:"username"`
}

func LoadConfig(filename string) (Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (cfg Config) Validate() error {
	if len(cfg.Routes) == 0 && cfg.MailAccount == nil {
		return errors.New("config must declare at least one route or module")
	}

	seen := make(map[string]struct{}, len(cfg.Routes))
	seenPaths := make(map[string]struct{}, len(cfg.Routes))
	for i, route := range cfg.Routes {
		if route.Path == "" {
			return fmt.Errorf("routes[%d].path is required", i)
		}
		if !strings.HasPrefix(route.Path, "/") {
			return fmt.Errorf("routes[%d].path must be absolute", i)
		}
		if path.Clean(route.Path) != route.Path {
			return fmt.Errorf("routes[%d].path must be clean", i)
		}
		if route.Target != "" && route.Target != RouteTargetBackend && route.Target != RouteTargetOverlayOnly {
			return fmt.Errorf("routes[%d].target must be %q or %q", i, RouteTargetBackend, RouteTargetOverlayOnly)
		}

		key := route.Path + "\x00" + route.Target
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate route path %q for target %q", route.Path, route.Target)
		}
		seen[key] = struct{}{}
		seenPaths[route.Path] = struct{}{}

		if route.Status != 0 {
			if route.Status < 300 || route.Status > 599 {
				return fmt.Errorf("routes[%d].status must be an HTTP error status from 300 through 599", i)
			}
			if route.Target == "" {
				return fmt.Errorf("routes[%d].status requires target", i)
			}
			if route.File != "" || route.Source != "" || route.SourceHost != "" || route.ContentType != "" || len(route.CacheStatuses) != 0 {
				return fmt.Errorf("routes[%d].status cannot be combined with file, source, source_host, content_type, or cache_statuses", i)
			}
			continue
		}

		if route.File != "" && route.Source != "" {
			return fmt.Errorf("routes[%d] cannot declare both file and source", i)
		}
		if route.File == "" && route.Source == "" {
			return fmt.Errorf("routes[%d] must declare file or source", i)
		}
		if route.File != "" {
			if strings.HasPrefix(route.File, "/") {
				return fmt.Errorf("routes[%d].file must be relative", i)
			}
			if path.Clean(route.File) != route.File || strings.HasPrefix(route.File, "../") || route.File == ".." {
				return fmt.Errorf("routes[%d].file must stay within the root", i)
			}
		}

		if route.Source != "" {
			if route.Source != RouteSourceBackend {
				return fmt.Errorf("routes[%d].source must be %q", i, RouteSourceBackend)
			}
			if route.Target != RouteTargetOverlayOnly {
				return fmt.Errorf("routes[%d] with backend source must target %q", i, RouteTargetOverlayOnly)
			}
			if err := validateSourceHost(route.SourceHost); err != nil {
				return fmt.Errorf("routes[%d].source_host: %w", i, err)
			}
			if route.ContentType != "" {
				return fmt.Errorf("routes[%d].content_type cannot override a backend source", i)
			}
		} else if route.SourceHost != "" {
			return fmt.Errorf("routes[%d].source_host requires source", i)
		} else if len(route.CacheStatuses) != 0 {
			return fmt.Errorf("routes[%d].cache_statuses requires source", i)
		}

		cacheStatuses := make(map[int]struct{}, len(route.CacheStatuses))
		for _, status := range route.CacheStatuses {
			if status < 100 || status > 599 {
				return fmt.Errorf("routes[%d].cache_statuses contains invalid HTTP status %d", i, status)
			}
			if _, ok := cacheStatuses[status]; ok {
				return fmt.Errorf("routes[%d].cache_statuses contains duplicate HTTP status %d", i, status)
			}
			cacheStatuses[status] = struct{}{}
		}
	}

	if cfg.MailAccount != nil {
		for _, modulePath := range cfg.MailAccount.Paths() {
			if _, ok := seenPaths[modulePath]; ok {
				return fmt.Errorf("mail_account route conflicts with static route %q", modulePath)
			}
			seenPaths[modulePath] = struct{}{}
		}
		if err := cfg.MailAccount.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func validateSourceHost(host string) error {
	if host == "" {
		return errors.New("is required for backend source")
	}
	if strings.TrimSpace(host) != host || strings.ContainsAny(host, "/\r\n\t") {
		return errors.New("must be a hostname without a scheme, path, or whitespace")
	}
	parsed, err := url.Parse("http://" + host)
	if err != nil || parsed.Host != host || parsed.Hostname() == "" || parsed.Port() != "" || parsed.User != nil {
		return errors.New("must be a hostname without a scheme or port")
	}
	return nil
}

func (cfg Config) ModulePaths() []string {
	var paths []string
	if cfg.MailAccount != nil {
		paths = append(paths, cfg.MailAccount.Paths()...)
	}
	return paths
}

func (cfg MailAccount) Validate() error {
	if len(cfg.Profiles) == 0 {
		return errors.New("mail_account.profiles must declare at least one profile")
	}

	seen := make(map[string]struct{}, len(cfg.Profiles))
	defaultCount := 0
	for i, profile := range cfg.Profiles {
		prefix := fmt.Sprintf("mail_account.profiles[%d]", i)
		match := strings.ToLower(profile.Match)
		if profile.Match == "" {
			return fmt.Errorf("%s.match is required", prefix)
		}
		if _, ok := seen[match]; ok {
			return fmt.Errorf("duplicate mail account profile match %q", profile.Match)
		}
		seen[match] = struct{}{}

		if match == defaultProfileMatch {
			defaultCount++
		} else {
			if strings.Contains(match, "/") {
				return fmt.Errorf("%s.match must not contain /", prefix)
			}
			if strings.ContainsAny(match, "[]\\") {
				return fmt.Errorf("%s.match only supports * and ? wildcards", prefix)
			}
			if !strings.Contains(match, "@") {
				return fmt.Errorf("%s.match must match an email address", prefix)
			}
			if _, err := path.Match(match, "user@example.org"); err != nil {
				return fmt.Errorf("%s.match is invalid: %w", prefix, err)
			}
		}

		if err := profile.Validate(prefix); err != nil {
			return err
		}
	}
	if defaultCount != 1 {
		return errors.New("mail_account.profiles must declare exactly one default profile")
	}
	if cfg.ManualSetup != nil {
		for i, section := range cfg.ManualSetup.ExtraSections {
			prefix := fmt.Sprintf("mail_account.manual_setup.extra_sections[%d]", i)
			if strings.TrimSpace(section.Title) == "" {
				return fmt.Errorf("%s.title is required", prefix)
			}
			if strings.TrimSpace(section.BodyMarkdown) == "" {
				return fmt.Errorf("%s.body_markdown is required", prefix)
			}
		}
	}

	return nil
}

func (cfg MailAccountProfile) Validate(prefix string) error {
	if cfg.Domain == "" {
		return fmt.Errorf("%s.domain is required", prefix)
	}
	if cfg.DisplayName == "" {
		return fmt.Errorf("%s.display_name is required", prefix)
	}
	if err := cfg.Incoming.Validate(prefix + ".incoming"); err != nil {
		return err
	}
	if err := cfg.Outgoing.Validate(prefix + ".outgoing"); err != nil {
		return err
	}
	return nil
}

func (cfg EmailServerConfig) Validate(prefix string) error {
	if cfg.Type == "" {
		return fmt.Errorf("%s.type is required", prefix)
	}
	if cfg.Hostname == "" {
		return fmt.Errorf("%s.hostname is required", prefix)
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("%s.port must be between 1 and 65535", prefix)
	}
	if cfg.SocketType == "" {
		return fmt.Errorf("%s.socket_type is required", prefix)
	}
	if cfg.Authentication == "" {
		return fmt.Errorf("%s.authentication is required", prefix)
	}
	if cfg.Username == "" {
		return fmt.Errorf("%s.username is required", prefix)
	}
	return nil
}
