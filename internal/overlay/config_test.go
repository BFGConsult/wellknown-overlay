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
			Profiles: []MailAccountProfile{{Match: "default"}},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected incomplete mail_account to be rejected")
	}
}

func TestConfigValidateRejectsMailAccountWithoutDefaultProfile(t *testing.T) {
	account := testMailAccount()
	account.Profiles[0].Match = "*@efn.no"

	cfg := Config{MailAccount: account}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing default profile to be rejected")
	}
}

func TestConfigValidateRejectsInvalidProfileMatch(t *testing.T) {
	account := testMailAccount()
	account.Profiles[0].Match = "efn.no"

	cfg := Config{MailAccount: account}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid profile match to be rejected")
	}
}

func TestConfigValidateRejectsUnsupportedProfileGlobSyntax(t *testing.T) {
	account := testMailAccount()
	account.Profiles[0].Match = "[ab]@efn.no"

	cfg := Config{MailAccount: account}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported profile glob syntax to be rejected")
	}
}

func TestConfigValidateRejectsIncompleteManualSetupExtraSection(t *testing.T) {
	account := testMailAccount()
	account.ManualSetup = &MailManualSetupConfig{
		ExtraSections: []MailManualSetupSection{
			{Title: "Password changes"},
		},
	}

	cfg := Config{MailAccount: account}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected incomplete manual setup extra section to be rejected")
	}
}

func testMailAccount() *MailAccount {
	return &MailAccount{
		Profiles: []MailAccountProfile{
			testMailAccountProfile("default", "EFN"),
		},
	}
}

func testMailAccountWithManualSetup() *MailAccount {
	account := testMailAccount()
	account.ManualSetup = &MailManualSetupConfig{
		URL: "https://autoconfig.efn.no/mail/setup",
	}
	return account
}

func testMailAccountProfile(match, displayName string) MailAccountProfile {
	return MailAccountProfile{
		Match:       match,
		Domain:      "efn.no",
		DisplayName: displayName,
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
