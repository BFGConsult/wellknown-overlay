package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHealthcheckCommandAcceptsOKEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if err := healthcheck([]string{"-url", server.URL}); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
}

func TestHealthcheckCommandRejectsUnhealthyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	if err := healthcheck([]string{"-url", server.URL}); err == nil {
		t.Fatal("expected healthcheck to reject non-200 endpoint")
	}
}

func TestValidateCommandReportsModuleRoutes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mail_account": {
    "profiles": [
      {
        "match": "default",
        "domain": "example.org",
        "display_name": "Example Mail",
        "incoming": {
          "type": "imap",
          "hostname": "mail.example.org",
          "port": 993,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        },
        "outgoing": {
          "type": "smtp",
          "hostname": "mail.example.org",
          "port": 587,
          "socket_type": "STARTTLS",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        }
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := validate([]string{"-config", configPath}, &stdout); err != nil {
		t.Fatal(err)
	}

	if got, want := stdout.String(), "ok: 0 route(s), 0 rule(s), 7 module route(s)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRenderCommandPassesQueryString(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(configPath, []byte(`{
  "mail_account": {
    "profiles": [
      {
        "match": "default",
        "domain": "example.org",
        "display_name": "Example Mail",
        "incoming": {
          "type": "imap",
          "hostname": "mail.example.org",
          "port": 993,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        },
        "outgoing": {
          "type": "smtp",
          "hostname": "mail.example.org",
          "port": 587,
          "socket_type": "STARTTLS",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        }
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := render([]string{
		"-config", configPath,
		"-root", dir,
		"-path", "/.well-known/mail/apple.mobileconfig?emailaddress=alice@example.org",
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(&stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("<string>alice@example.org</string>")) {
		t.Fatalf("render output does not contain substituted email address:\n%s", body)
	}
}
