package overlay

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

type Config struct {
	Routes      []Route      `json:"routes,omitempty"`
	MailAccount *MailAccount `json:"mail_account,omitempty"`
}

type Route struct {
	Path        string `json:"path"`
	File        string `json:"file"`
	ContentType string `json:"content_type,omitempty"`
}

type MailAccount struct {
	ManualSetup *MailManualSetupConfig `json:"manual_setup,omitempty"`
	Profiles    []MailAccountProfile   `json:"profiles"`
}

type MailManualSetupConfig struct {
	URL           string                   `json:"url,omitempty"`
	ExtraSections []MailManualSetupSection `json:"extra_sections,omitempty"`
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
		if _, ok := seen[route.Path]; ok {
			return fmt.Errorf("duplicate route path %q", route.Path)
		}
		seen[route.Path] = struct{}{}

		if route.File == "" {
			return fmt.Errorf("routes[%d].file is required", i)
		}
		if strings.HasPrefix(route.File, "/") {
			return fmt.Errorf("routes[%d].file must be relative", i)
		}
		if path.Clean(route.File) != route.File || strings.HasPrefix(route.File, "../") || route.File == ".." {
			return fmt.Errorf("routes[%d].file must stay within the root", i)
		}
	}

	if cfg.MailAccount != nil {
		for _, modulePath := range MailAccountPaths() {
			if _, ok := seen[modulePath]; ok {
				return fmt.Errorf("mail_account route conflicts with static route %q", modulePath)
			}
			seen[modulePath] = struct{}{}
		}
		if err := cfg.MailAccount.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (cfg Config) ModulePaths() []string {
	var paths []string
	if cfg.MailAccount != nil {
		paths = append(paths, MailAccountPaths()...)
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
