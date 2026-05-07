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

func TestConfigValidateAcceptsEmailAutoconfigWithoutStaticRoutes(t *testing.T) {
	cfg := Config{
		EmailAutoconfig: testEmailAutoconfig(),
	}

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidateRejectsEmailAutoconfigStaticRouteConflict(t *testing.T) {
	cfg := Config{
		Routes: []Route{
			{Path: EmailAutoconfigWellKnownPath, File: "config-v1.1.xml"},
		},
		EmailAutoconfig: testEmailAutoconfig(),
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected email_autoconfig route conflict to be rejected")
	}
}

func TestConfigValidateRejectsIncompleteEmailAutoconfig(t *testing.T) {
	cfg := Config{
		EmailAutoconfig: &EmailAutoconfig{
			Domain:      "example.org",
			DisplayName: "Example Mail",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected incomplete email_autoconfig to be rejected")
	}
}

func testEmailAutoconfig() *EmailAutoconfig {
	return &EmailAutoconfig{
		Domain:      "efn.no",
		DisplayName: "EFN",
		Incoming: EmailServerConfig{
			Type:           "imap",
			Hostname:       "login.kristshell.net",
			Port:           993,
			SocketType:     "SSL",
			Authentication: "password-cleartext",
			Username:       "%EMAILADDRESS%",
		},
		Outgoing: EmailServerConfig{
			Type:           "smtp",
			Hostname:       "login.kristshell.net",
			Port:           587,
			SocketType:     "STARTTLS",
			Authentication: "password-cleartext",
			Username:       "%EMAILADDRESS%",
		},
	}
}
