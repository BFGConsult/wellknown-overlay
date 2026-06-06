package overlay

import (
	"strings"
	"testing"
)

func TestRenderAppleMobileconfigUsesMailAccountSettings(t *testing.T) {
	body, err := RenderAppleMobileconfig(testMailAccount().SelectProfile("bfg@example.org"), "bfg@example.org")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	wantSubstrings := []string{
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`,
		"<key>PayloadType</key>",
		"<string>com.apple.mail.managed</string>",
		"<key>EmailAddress</key>",
		"<string>bfg@example.org</string>",
		"<key>EmailAccountType</key>",
		"<string>EmailTypeIMAP</string>",
		"<key>IncomingMailServerHostName</key>",
		"<string>mail.example.org</string>",
		"<key>IncomingMailServerPortNumber</key>",
		"<integer>993</integer>",
		"<key>IncomingMailServerUseSSL</key>",
		"<true/>",
		"<key>IncomingMailServerUsername</key>",
		"<string>bfg@example.org</string>",
		"<key>OutgoingMailServerPortNumber</key>",
		"<integer>587</integer>",
		"<key>OutgoingPasswordSameAsIncomingPassword</key>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Fatalf("mobileconfig does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderAppleMobileconfigEscapesXML(t *testing.T) {
	cfg := testMailAccount().SelectProfile("person+test@example.org")
	cfg.DisplayName = "A&B <Mail>"

	body, err := RenderAppleMobileconfig(cfg, "person+test@example.org")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	if !strings.Contains(got, "<string>A&amp;B &lt;Mail&gt;</string>") {
		t.Fatalf("mobileconfig did not escape display name:\n%s", got)
	}
}
