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
	Routes []Route `json:"routes"`
}

type Route struct {
	Path        string `json:"path"`
	File        string `json:"file"`
	ContentType string `json:"content_type,omitempty"`
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
	if len(cfg.Routes) == 0 {
		return errors.New("config must declare at least one route")
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

	return nil
}
