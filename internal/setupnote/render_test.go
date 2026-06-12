package setupnote

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

func TestRenderMarkdownUsesLiteralPlaceholdersAndOmitsPassword(t *testing.T) {
	body, err := Render(fstest.MapFS{}, testConfig(), Options{Mode: ModeMarkdown})
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	for _, want := range []string{
		"# Email setup for Example Mail",
		"Your email address: %EMAILADDRESS%",
		"| Username | %USERNAME% |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Password:") {
		t.Fatalf("password should be omitted when not supplied:\n%s", got)
	}
}

func TestRenderMarkdownUsesTemplateOverrideAndPassword(t *testing.T) {
	files := fstest.MapFS{
		"mail-setup.md": {Data: []byte("Custom {{display_name}} {{email_address}} {{incoming.username}}\n")},
	}

	body, err := Render(files, testConfig(), Options{
		Mode:     ModeMarkdown,
		Email:    "alice@example.org",
		Username: "login@example.org",
		Password: "UNIQUE-OFFLINE-PASSWORD-1f2e3d4c",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	for _, want := range []string{
		"Custom Example Mail alice@example.org login@example.org",
		"Password: UNIQUE-OFFLINE-PASSWORD-1f2e3d4c",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderMarkdownUsesTranslation(t *testing.T) {
	body, err := Render(fstest.MapFS{}, testConfig(), Options{
		Mode: ModeMarkdown,
		Lang: "nb",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	if !strings.Contains(got, "# E-postoppsett for Example Mail") {
		t.Fatalf("body does not use Norwegian translation:\n%s", got)
	}
	if !strings.Contains(got, "%EMAILADDRESS%") || !strings.Contains(got, "%USERNAME%") {
		t.Fatalf("body should use literal placeholders:\n%s", got)
	}
}

func TestRenderTextEmitsStableColonSeparatedFields(t *testing.T) {
	body, err := Render(fstest.MapFS{}, testConfig(), Options{
		Mode:     ModeText,
		Email:    "alice@example.org",
		Username: "alice-login",
		Password: "UNIQUE-TEXT-PASSWORD-4c5b6a",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	want := strings.Join([]string{
		"Display-name: Example Mail",
		"Email-address: alice@example.org",
		"Username: alice-login",
		"Password: UNIQUE-TEXT-PASSWORD-4c5b6a",
		"IMAP-type: imap",
		"IMAP-server: mail.example.org",
		"IMAP-port: 993",
		"IMAP-security: SSL",
		"IMAP-authentication: password-cleartext",
		"IMAP-username: alice-login",
		"SMTP-type: smtp",
		"SMTP-server: smtp.example.org",
		"SMTP-port: 465",
		"SMTP-security: SSL",
		"SMTP-authentication: password-cleartext",
		"SMTP-username: alice-login",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("text output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTextOmitsPasswordAndUsesPlaceholders(t *testing.T) {
	body, err := Render(fstest.MapFS{}, testConfig(), Options{Mode: ModeText})
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	if !strings.Contains(got, "Email-address: %EMAILADDRESS%\n") || !strings.Contains(got, "Username: %USERNAME%\n") {
		t.Fatalf("text output should use placeholders:\n%s", got)
	}
	if strings.Contains(got, "Password:") {
		t.Fatalf("password should be omitted when not supplied:\n%s", got)
	}
}

func TestRenderSelectsProfileByEmailAndHost(t *testing.T) {
	emailBody, err := Render(fstest.MapFS{}, testConfig(), Options{
		Mode:  ModeText,
		Email: "person+help@example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(emailBody), "Display-name: Helpdesk\n") {
		t.Fatalf("email should select help profile:\n%s", emailBody)
	}

	hostBody, err := Render(fstest.MapFS{}, testConfig(), Options{
		Mode: ModeText,
		Host: "autoconfig.invest.example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostBody), "Display-name: Invest Mail\n") {
		t.Fatalf("host should select invest profile:\n%s", hostBody)
	}
}

func TestRenderRejectsMissingMailAccountAndInvalidMode(t *testing.T) {
	if _, err := Render(fstest.MapFS{}, overlay.Config{}, Options{Mode: ModeMarkdown}); err == nil {
		t.Fatal("expected missing mail_account error")
	}
	if _, err := Render(fstest.MapFS{}, testConfig(), Options{Mode: Mode("bad")}); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func testConfig() overlay.Config {
	return overlay.Config{
		MailAccount: &overlay.MailAccount{
			Profiles: []overlay.MailAccountProfile{
				{
					Match:            "*+help@example.org",
					Domain:           "example.org",
					DisplayName:      "Helpdesk",
					DisplayShortName: "Help",
					Incoming:         testServer("imap", "help.example.org", 993, "SSL", "help@example.org"),
					Outgoing:         testServer("smtp", "smtp-help.example.org", 587, "STARTTLS", "help@example.org"),
				},
				{
					Match:            "*@invest.example.org",
					Domain:           "invest.example.org",
					DisplayName:      "Invest Mail",
					DisplayShortName: "Invest",
					Incoming:         testServer("imap", "invest.example.org", 993, "SSL", "%EMAILADDRESS%"),
					Outgoing:         testServer("smtp", "smtp-invest.example.org", 465, "SSL", "%EMAILADDRESS%"),
				},
				{
					Match:            "default",
					Domain:           "example.org",
					DisplayName:      "Example Mail",
					DisplayShortName: "Example",
					Incoming:         testServer("imap", "mail.example.org", 993, "SSL", "%EMAILADDRESS%"),
					Outgoing:         testServer("smtp", "smtp.example.org", 465, "SSL", "%EMAILADDRESS%"),
				},
			},
		},
	}
}

func testServer(serverType, hostname string, port int, socketType, username string) overlay.EmailServerConfig {
	return overlay.EmailServerConfig{
		Type:           serverType,
		Hostname:       hostname,
		Port:           port,
		SocketType:     socketType,
		Authentication: "password-cleartext",
		Username:       username,
	}
}
