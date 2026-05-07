package main

import (
	"bytes"
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
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := validate([]string{"-config", configPath}, &stdout); err != nil {
		t.Fatal(err)
	}

	if got, want := stdout.String(), "ok: 0 route(s), 2 module route(s)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
