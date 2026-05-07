package overlay

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestResponderRendersConfiguredStaticRoute(t *testing.T) {
	cfg := Config{
		Routes: []Route{
			{
				Path:        "/.well-known/security.txt",
				File:        "security.txt",
				ContentType: "text/plain; charset=utf-8",
			},
		},
	}
	files := fstest.MapFS{
		"security.txt": {Data: []byte("Contact: mailto:security@example.org\n")},
	}

	responder := NewResponder(cfg, files)
	response, err := responder.Render("/.well-known/security.txt")
	if err != nil {
		t.Fatal(err)
	}

	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Status, http.StatusOK)
	}
	if string(response.Body) != "Contact: mailto:security@example.org\n" {
		t.Fatalf("unexpected body %q", response.Body)
	}
	if response.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", response.ContentType)
	}
}

func TestResponderRejectsUndeclaredRoute(t *testing.T) {
	responder := NewResponder(Config{
		Routes: []Route{{Path: "/declared", File: "declared.txt"}},
	}, fstest.MapFS{})

	_, err := responder.Render("/other")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResponderRendersMailAccountRoutes(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{})

	wellKnownResponse, err := responder.Render(ThunderbirdAutoconfigWellKnownPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyResponse, err := responder.Render(ThunderbirdAutoconfigLegacyPath)
	if err != nil {
		t.Fatal(err)
	}

	if wellKnownResponse.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", wellKnownResponse.Status, http.StatusOK)
	}
	if wellKnownResponse.ContentType != "application/xml" {
		t.Fatalf("content type = %q, want application/xml", wellKnownResponse.ContentType)
	}
	if string(wellKnownResponse.Body) != string(legacyResponse.Body) {
		t.Fatal("email autoconfig routes served different bodies")
	}

	body := string(wellKnownResponse.Body)
	wantSubstrings := []string{
		`<emailProvider id="efn.no">`,
		"<domain>efn.no</domain>",
		"<displayName>EFN</displayName>",
		`<incomingServer type="imap">`,
		"<hostname>login.kristshell.net</hostname>",
		"<port>993</port>",
		"<socketType>SSL</socketType>",
		`<outgoingServer type="smtp">`,
		"<port>587</port>",
		"<socketType>STARTTLS</socketType>",
		"<username>%EMAILADDRESS%</username>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q:\n%s", want, body)
		}
	}
}

func TestResponderRendersAppleMobileconfigRoute(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{})

	response, err := responder.RenderRequest(AppleMobileconfigPath, map[string][]string{
		"emailaddress": {"bfg@efn.no"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Status, http.StatusOK)
	}
	if response.ContentType != "application/x-apple-aspen-config" {
		t.Fatalf("content type = %q, want application/x-apple-aspen-config", response.ContentType)
	}
	body := string(response.Body)
	if !strings.Contains(body, "<key>EmailAddress</key>") || !strings.Contains(body, "<string>bfg@efn.no</string>") {
		t.Fatalf("mobileconfig does not contain substituted email address:\n%s", body)
	}
}
