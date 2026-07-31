package overlay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigValidateAcceptsOrderedGatewayRules(t *testing.T) {
	var cfg Config
	err := json.Unmarshal([]byte(`{
  "rules": [
    {"match":{"path":"/robots.txt"},"file":"robots.txt","content_type":"text/plain"},
    {"match":{"hosts":["efnu.no"]},"redirect":{"origin":"https://efn.no","status":308,"preserve_request_uri":true}},
    {"match":"*","status":404}
  ]
}`), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if !cfg.Rules[2].Match.All {
		t.Fatal(`expected "match": "*" to set unconditional match`)
	}
}

func TestConfigValidateRejectsInvalidGatewayRules(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		want       string
	}{
		{name: "missing match", configJSON: `{"rules":[{"status":404}]}`, want: "match is required"},
		{name: "empty match", configJSON: `{"rules":[{"match":{},"status":404}]}`, want: "empty match is invalid"},
		{name: "empty hosts", configJSON: `{"rules":[{"match":{"hosts":[]},"status":404}]}`, want: "must not be empty"},
		{name: "host wildcard", configJSON: `{"rules":[{"match":{"hosts":["*"]},"status":404}]}`, want: "omit hosts"},
		{name: "path and prefix", configJSON: `{"rules":[{"match":{"path":"/a","path_prefix":"/"},"status":404}]}`, want: "both path and path_prefix"},
		{name: "multiple actions", configJSON: `{"rules":[{"match":"*","status":404,"file":"error.txt"}]}`, want: "exactly one"},
		{name: "reserved rewrite", configJSON: `{"rules":[{"match":"*","rewrite":{"path":"/new"}}]}`, want: "reserved but not supported"},
		{name: "redirect path", configJSON: `{"rules":[{"match":"*","redirect":{"origin":"https://example.org/path","status":308}}]}`, want: "must not contain"},
		{name: "unknown match field", configJSON: `{"rules":[{"match":{"method":"GET"},"status":404}]}`, want: "unknown match field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(tt.configJSON), &cfg)
			if err == nil {
				err = cfg.Validate()
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

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

func TestConfigValidateAcceptsSamePathForDifferentTargets(t *testing.T) {
	cfg := Config{Routes: []Route{
		{Path: "/robots.txt", File: "robots-backend.txt", Target: RouteTargetBackend},
		{Path: "/robots.txt", File: "robots-overlay.txt", Target: RouteTargetOverlayOnly},
	}}

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidateAcceptsBackendSourceForOverlayOnlyTarget(t *testing.T) {
	cfg := Config{Routes: []Route{{
		Path:          "/robots.txt",
		Target:        RouteTargetOverlayOnly,
		Source:        RouteSourceBackend,
		SourceHost:    "example.org",
		CacheStatuses: []int{200, 404},
	}}}

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidateAcceptsTargetedErrorStatus(t *testing.T) {
	cfg := Config{Routes: []Route{{
		Path:   "/sitemap.xml",
		Target: RouteTargetOverlayOnly,
		Status: 404,
	}}}

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidateRejectsInvalidTargetedSources(t *testing.T) {
	tests := []struct {
		name  string
		route Route
	}{
		{
			name:  "file and source",
			route: Route{Path: "/robots.txt", File: "robots.txt", Target: RouteTargetOverlayOnly, Source: RouteSourceBackend, SourceHost: "example.org"},
		},
		{
			name:  "backend source on backend target",
			route: Route{Path: "/robots.txt", Target: RouteTargetBackend, Source: RouteSourceBackend, SourceHost: "example.org"},
		},
		{
			name:  "backend source without host",
			route: Route{Path: "/robots.txt", Target: RouteTargetOverlayOnly, Source: RouteSourceBackend},
		},
		{
			name:  "source host with scheme",
			route: Route{Path: "/robots.txt", Target: RouteTargetOverlayOnly, Source: RouteSourceBackend, SourceHost: "https://example.org"},
		},
		{
			name:  "cache statuses without source",
			route: Route{Path: "/robots.txt", File: "robots.txt", Target: RouteTargetOverlayOnly, CacheStatuses: []int{404}},
		},
		{
			name:  "invalid cache status",
			route: Route{Path: "/robots.txt", Target: RouteTargetOverlayOnly, Source: RouteSourceBackend, SourceHost: "example.org", CacheStatuses: []int{700}},
		},
		{
			name:  "error status without target",
			route: Route{Path: "/sitemap.xml", Status: 404},
		},
		{
			name:  "error status with source",
			route: Route{Path: "/sitemap.xml", Target: RouteTargetOverlayOnly, Status: 404, Source: RouteSourceBackend, SourceHost: "example.org"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := (Config{Routes: []Route{tt.route}}).Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
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
	account.Profiles[0].Match = "*@example.org"

	cfg := Config{MailAccount: account}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing default profile to be rejected")
	}
}

func TestConfigValidateRejectsInvalidProfileMatch(t *testing.T) {
	account := testMailAccount()
	account.Profiles[0].Match = "example.org"

	cfg := Config{MailAccount: account}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid profile match to be rejected")
	}
}

func TestConfigValidateRejectsUnsupportedProfileGlobSyntax(t *testing.T) {
	account := testMailAccount()
	account.Profiles[0].Match = "[ab]@example.org"

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
		URL: "https://autoconfig.example.org/mail/setup",
	}
	return account
}

func testMailAccountWithSharePreview() *MailAccount {
	account := testMailAccountWithManualSetup()
	account.ManualSetup.SharePreview = &MailSharePreviewConfig{Enabled: true}
	return account
}

func testMailAccountProfile(match, displayName string) MailAccountProfile {
	return MailAccountProfile{
		Match:       match,
		Domain:      "example.org",
		DisplayName: displayName,
		Incoming: EmailServerConfig{
			Type:           "imap",
			Hostname:       "mail.example.org",
			Port:           993,
			SocketType:     "SSL",
			Authentication: "password-cleartext",
			Username:       "%EMAILADDRESS%",
		},
		Outgoing: EmailServerConfig{
			Type:           "smtp",
			Hostname:       "mail.example.org",
			Port:           587,
			SocketType:     "STARTTLS",
			Authentication: "password-cleartext",
			Username:       "%EMAILADDRESS%",
		},
	}
}
