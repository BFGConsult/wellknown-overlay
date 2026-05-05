package overlay

import "testing"

func TestConfigValidateRejectsDuplicateRoutes(t *testing.T) {
	cfg := Config{
		Routes: []Route{
			{Path: "/.well-known/security.txt", File: "security.txt"},
			{Path: "/.well-known/security.txt", File: "other.txt"},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected duplicate route to be rejected")
	}
}

func TestConfigValidateRejectsEscapingFiles(t *testing.T) {
	cfg := Config{
		Routes: []Route{
			{Path: "/.well-known/security.txt", File: "../security.txt"},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected escaping file path to be rejected")
	}
}
