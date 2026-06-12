package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesMarkdownToStdout(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-root", dir,
		"-email", "alice@example.org",
		"-username", "alice-login",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}

	got := stdout.String()
	for _, want := range []string{
		"# Email setup for Example Mail",
		"Your email address: alice@example.org",
		"| Username | alice-login |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, got)
		}
	}
}

func TestRunWritesOutputFile(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)
	outputPath := filepath.Join(dir, "setup.txt")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-root", dir,
		"-mode", "text",
		"-username", "alice-login",
		"-password", "UNIQUE-CLI-PASSWORD-95b3",
		"-output", outputPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout when -output is used, got %q", stdout.String())
	}

	body, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "Password: UNIQUE-CLI-PASSWORD-95b3\n") {
		t.Fatalf("output file does not contain password:\n%s", got)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("output permissions = %v, want 0600", gotMode)
	}
}

func TestRunReadsPasswordFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)
	t.Setenv(envPassword, "UNIQUE-ENV-PASSWORD-7a8b")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-mode", "text",
		"-username", "alice-login",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Password: UNIQUE-ENV-PASSWORD-7a8b\n") {
		t.Fatalf("stdout does not contain env password:\n%s", stdout.String())
	}
}

func TestRunReadsSingleCredentialsEntryWithoutMachine(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)
	credentialsPath := filepath.Join(dir, "setup-note.netrc")
	if err := os.WriteFile(credentialsPath, []byte("login file-user password UNIQUE-FILE-PASSWORD-1a2b\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-mode", "text",
		"-credentials", credentialsPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{
		"Username: file-user\n",
		"Password: UNIQUE-FILE-PASSWORD-1a2b\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, got)
		}
	}
}

func TestRunSelectsCredentialsByMachine(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)
	credentialsPath := filepath.Join(dir, "setup-note.netrc")
	if err := os.WriteFile(credentialsPath, []byte(`
machine other.example.org login wrong password WRONG-PASSWORD
machine example.org login machine-user password UNIQUE-MACHINE-PASSWORD-3c4d
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-mode", "text",
		"-email", "alice@example.org",
		"-credentials", credentialsPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "Username: machine-user\n") || !strings.Contains(got, "Password: UNIQUE-MACHINE-PASSWORD-3c4d\n") {
		t.Fatalf("stdout does not contain selected credentials:\n%s", got)
	}
	if strings.Contains(got, "WRONG-PASSWORD") {
		t.Fatalf("stdout contains unselected credentials:\n%s", got)
	}
}

func TestRunDefaultsToHomeNetrcWhenPresent(t *testing.T) {
	dir := t.TempDir()
	home := isolateHome(t)
	configPath := writeTestConfig(t, dir, true)
	if err := os.WriteFile(filepath.Join(home, ".netrc"), []byte("login netrc-user password UNIQUE-NETRC-PASSWORD-5e6f\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-mode", "text",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "Username: netrc-user\n") || !strings.Contains(got, "Password: UNIQUE-NETRC-PASSWORD-5e6f\n") {
		t.Fatalf("stdout does not contain ~/.netrc credentials:\n%s", got)
	}
}

func TestRunIgnoresMissingDefaultNetrc(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-config", configPath,
		"-mode", "text",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	got := stdout.String()
	if strings.Contains(got, "Password:") {
		t.Fatalf("missing default ~/.netrc should not add password:\n%s", got)
	}
}

func TestRunFailsForMissingExplicitCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)

	err := run([]string{
		"-config", configPath,
		"-credentials", filepath.Join(dir, "missing.netrc"),
	}, ioDiscard{}, ioDiscard{})
	if err == nil {
		t.Fatal("expected missing explicit credentials file to fail")
	}
}

func TestRunRejectsInvalidModeAndMissingMailAccount(t *testing.T) {
	dir := t.TempDir()
	isolateHome(t)
	configPath := writeTestConfig(t, dir, true)

	if err := run([]string{"-config", configPath, "-mode", "bad"}, ioDiscard{}, ioDiscard{}); err == nil {
		t.Fatal("expected invalid mode error")
	}

	noMailConfig := writeTestConfig(t, dir, false)
	if err := run([]string{"-config", noMailConfig}, ioDiscard{}, ioDiscard{}); err == nil {
		t.Fatal("expected missing mail_account error")
	}
}

func writeTestConfig(t *testing.T, dir string, mailAccount bool) string {
	t.Helper()
	configPath := filepath.Join(dir, "overlay.json")
	body := `{"routes":[{"path":"/x","file":"x.txt"}]}`
	if mailAccount {
		body = `{
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
          "hostname": "smtp.example.org",
          "port": 465,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        }
      }
    ]
  }
}`
	}
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	previous := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = previous })
	return home
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
