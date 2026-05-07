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

func TestConfigValidateAcceptsMailAccountWithoutStaticRoutes(t *testing.T) {
	cfg := Config{
		MailAccount: testMailAccount(),
	}

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidateRejectsMailAccountStaticRouteConflict(t *testing.T) {
	cfg := Config{
		Routes: []Route{
			{Path: ThunderbirdAutoconfigWellKnownPath, File: "config-v1.1.xml"},
		},
		MailAccount: testMailAccount(),
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected mail_account route conflict to be rejected")
	}
}

func TestConfigValidateRejectsIncompleteMailAccount(t *testing.T) {
	cfg := Config{
		MailAccount: &MailAccount{
			Domain:      "example.org",
			DisplayName: "Example Mail",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected incomplete mail_account to be rejected")
	}
}

func testMailAccount() *MailAccount {
	return &MailAccount{
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
