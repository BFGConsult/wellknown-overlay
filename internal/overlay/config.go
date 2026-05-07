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
	Routes          []Route          `json:"routes,omitempty"`
	EmailAutoconfig *EmailAutoconfig `json:"email_autoconfig,omitempty"`
}

type Route struct {
	Path        string `json:"path"`
	File        string `json:"file"`
	ContentType string `json:"content_type,omitempty"`
}

type EmailAutoconfig struct {
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
	if len(cfg.Routes) == 0 && cfg.EmailAutoconfig == nil {
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

	if cfg.EmailAutoconfig != nil {
		for _, modulePath := range EmailAutoconfigPaths() {
			if _, ok := seen[modulePath]; ok {
				return fmt.Errorf("email_autoconfig route conflicts with static route %q", modulePath)
			}
			seen[modulePath] = struct{}{}
		}
		if err := cfg.EmailAutoconfig.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (cfg Config) ModulePaths() []string {
	var paths []string
	if cfg.EmailAutoconfig != nil {
		paths = append(paths, EmailAutoconfigPaths()...)
	}
	return paths
}

func (cfg EmailAutoconfig) Validate() error {
	if cfg.Domain == "" {
		return errors.New("email_autoconfig.domain is required")
	}
	if cfg.DisplayName == "" {
		return errors.New("email_autoconfig.display_name is required")
	}
	if err := cfg.Incoming.Validate("email_autoconfig.incoming"); err != nil {
		return err
	}
	if err := cfg.Outgoing.Validate("email_autoconfig.outgoing"); err != nil {
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
