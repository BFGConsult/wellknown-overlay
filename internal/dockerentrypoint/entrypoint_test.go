package dockerentrypoint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareOverlayConfigGeneratesMailAccountFromEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "overlay.json")
	setMailEnv(t)

	var stderr bytes.Buffer
	if err := PrepareOverlayConfig(configPath, &stderr); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), "generated overlay config from MAIL_* variables") {
		t.Fatalf("stderr = %q, want generated config message", stderr.String())
	}

	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"domain": "example.org"`,
		`"display_name": "Example Mail"`,
		`"hostname": "imap.example.org"`,
		`"hostname": "smtp.example.org"`,
		`"username": "%EMAILADDRESS%"`,
		`"manual_setup"`,
		`"url": "https://autoconfig.example.org/mail/setup"`,
		`"extra_sections"`,
		`"title": "Password changes"`,
		`"body_markdown": "Change your password in the [mail admin](https://admin.example.org/)."`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("generated config does not contain %q:\n%s", want, body)
		}
	}
}

func TestPrepareOverlayConfigUsesExistingConfigAndWarnsIgnoredMailEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(configPath, []byte(`{
  "routes": [
    {
      "path": "/.well-known/security.txt",
      "file": "security.txt"
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAIL_DOMAIN", "example.org")

	var stderr bytes.Buffer
	if err := PrepareOverlayConfig(configPath, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "warning: MAIL_* variables ignored") {
		t.Fatalf("stderr = %q, want ignored MAIL warning", stderr.String())
	}
}

func TestPrepareOverlayConfigRejectsMissingConfigAndIncompleteEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "overlay.json")

	var stderr bytes.Buffer
	err := PrepareOverlayConfig(configPath, &stderr)
	if err == nil {
		t.Fatal("expected missing config and env to fail")
	}
	if !strings.Contains(err.Error(), "environment config is incomplete") {
		t.Fatalf("err = %v, want incomplete env error", err)
	}
}

func TestCoreWarnsForGatewayOnlyEnv(t *testing.T) {
	t.Setenv("BACKEND_URL", "http://app:8080")
	t.Setenv("VIRTUAL_HOST", "example.org")

	var stderr bytes.Buffer
	WarnGatewayOnlyEnv(&stderr)

	got := stderr.String()
	if !strings.Contains(got, "BACKEND_URL only affects the gateway image") {
		t.Fatalf("stderr = %q, want BACKEND_URL warning", got)
	}
	if strings.Contains(got, "VIRTUAL_HOST") {
		t.Fatalf("stderr = %q, did not expect external proxy env warning", got)
	}
}

func TestRunCoreExecsOverlayServe(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "overlay.json")
	setMailEnv(t)

	var stderr bytes.Buffer
	var gotPath string
	var gotArgs []string
	execer := func(path string, args []string, env []string) error {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		return nil
	}

	err := Run([]string{
		"wellknown-overlay-docker-entrypoint",
		"core",
		"-config", configPath,
		"-root", "/tmp/public",
		"-listen", "127.0.0.1:9000",
	}, &stderr, execer)
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != OverlayBinary {
		t.Fatalf("exec path = %q, want %q", gotPath, OverlayBinary)
	}
	wantArgs := []string{
		"wellknown-overlay",
		"serve",
		"-config", configPath,
		"-root", "/tmp/public",
		"-listen", "127.0.0.1:9000",
	}
	if strings.Join(gotArgs, "\n") != strings.Join(wantArgs, "\n") {
		t.Fatalf("exec args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func setMailEnv(t *testing.T) {
	t.Helper()

	t.Setenv("MAIL_DOMAIN", "example.org")
	t.Setenv("MAIL_DISPLAY_NAME", "Example Mail")
	t.Setenv("MAIL_INCOMING_HOST", "imap.example.org")
	t.Setenv("MAIL_OUTGOING_HOST", "smtp.example.org")
	t.Setenv("MAIL_SETUP_URL", "https://autoconfig.example.org/mail/setup")
	t.Setenv("MAIL_SETUP_EXTRA_SECTION_1_TITLE", "Password changes")
	t.Setenv("MAIL_SETUP_EXTRA_SECTION_1_BODY_MARKDOWN", "Change your password in the [mail admin](https://admin.example.org/).")
}
