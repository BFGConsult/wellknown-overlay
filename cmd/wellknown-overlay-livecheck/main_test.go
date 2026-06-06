package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseDKIMSelectorsUsesCLIValue(t *testing.T) {
	got := parseDKIMSelectors("mail2026, default ,mail2026", "envselector")
	want := []string{"mail2026", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectors = %#v, want %#v", got, want)
	}
}

func TestParseDKIMSelectorsFallsBackToEnvValue(t *testing.T) {
	got := parseDKIMSelectors("", "mail2026,default")
	want := []string{"mail2026", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectors = %#v, want %#v", got, want)
	}
}

func TestParseDKIMSelectorsReturnsNilWhenUnset(t *testing.T) {
	if got := parseDKIMSelectors("", ""); got != nil {
		t.Fatalf("selectors = %#v, want nil", got)
	}
}

func TestRoundTripPasswordUsesEnvWhenEnabled(t *testing.T) {
	got, err := roundTripPassword(true, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Fatalf("password = %q, want secret", got)
	}
}

func TestRoundTripPasswordIgnoresEnvWhenDisabled(t *testing.T) {
	got, err := roundTripPassword(false, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("password = %q, want empty", got)
	}
}

func TestRoundTripPasswordFailsWithoutEnvInNonInteractiveRun(t *testing.T) {
	originalIsTerminal := stdinIsTerminal
	originalReadPassword := readPassword
	t.Cleanup(func() {
		stdinIsTerminal = originalIsTerminal
		readPassword = originalReadPassword
	})
	stdinIsTerminal = func(fd int) bool { return false }
	readPassword = func(fd int) ([]byte, error) { return nil, errors.New("should not read") }

	if _, err := roundTripPassword(true, ""); err == nil {
		t.Fatal("expected password error")
	}
}

func TestReadLivecheckConfigParsesKeyValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "livecheck.local")
	if err := os.WriteFile(path, []byte(`
# local secrets
LIVECHECK_PASSWORD='secret value'
LIVECHECK_RECEIVER_EMAIL=receiver@example.net
LIVECHECK_RECEIVER_PASSWORD='receiver secret'
DKIM_SELECTORS="mail2026,default"
UNQUOTED=value
`), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := readLivecheckConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := config["LIVECHECK_PASSWORD"], "secret value"; got != want {
		t.Fatalf("password = %q, want %q", got, want)
	}
	if got, want := config["DKIM_SELECTORS"], "mail2026,default"; got != want {
		t.Fatalf("selectors = %q, want %q", got, want)
	}
	if got, want := config["LIVECHECK_RECEIVER_EMAIL"], "receiver@example.net"; got != want {
		t.Fatalf("receiver email = %q, want %q", got, want)
	}
	if got, want := config["LIVECHECK_RECEIVER_PASSWORD"], "receiver secret"; got != want {
		t.Fatalf("receiver password = %q, want %q", got, want)
	}
	if got, want := config["UNQUOTED"], "value"; got != want {
		t.Fatalf("unquoted = %q, want %q", got, want)
	}
}

func TestReadLivecheckConfigRejectsInvalidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "livecheck.local")
	if err := os.WriteFile(path, []byte("LIVECHECK_PASSWORD\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readLivecheckConfig(path); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestConfigValuePrefersEnvironment(t *testing.T) {
	t.Setenv("LIVECHECK_PASSWORD", "env-secret")
	got := configValue("LIVECHECK_PASSWORD", map[string]string{"LIVECHECK_PASSWORD": "file-secret"})
	if got != "env-secret" {
		t.Fatalf("config value = %q, want env-secret", got)
	}
}

func TestConfigValueFallsBackToFile(t *testing.T) {
	got := configValue("LIVECHECK_PASSWORD", map[string]string{"LIVECHECK_PASSWORD": "file-secret"})
	if got != "file-secret" {
		t.Fatalf("config value = %q, want file-secret", got)
	}
}

func TestResolveEmailAndProfileArgsUsesConfigEmail(t *testing.T) {
	email, profiles := resolveEmailAndProfileArgs(nil, map[string]string{"LIVECHECK_EMAIL": "alice@example.org"})
	if email != "alice@example.org" {
		t.Fatalf("email = %q, want alice@example.org", email)
	}
	if profiles != nil {
		t.Fatalf("profiles = %#v, want nil", profiles)
	}
}

func TestResolveEmailAndProfileArgsPrefersPositionalEmail(t *testing.T) {
	email, profiles := resolveEmailAndProfileArgs([]string{"bob@example.org", "APPLE"}, map[string]string{"LIVECHECK_EMAIL": "alice@example.org"})
	if email != "bob@example.org" {
		t.Fatalf("email = %q, want bob@example.org", email)
	}
	if !reflect.DeepEqual(profiles, []string{"APPLE"}) {
		t.Fatalf("profiles = %#v, want APPLE", profiles)
	}
}

func TestResolveEmailAndProfileArgsTreatsProfileAsProfileWhenConfigEmailSet(t *testing.T) {
	email, profiles := resolveEmailAndProfileArgs([]string{"ALL"}, map[string]string{"LIVECHECK_EMAIL": "alice@example.org"})
	if email != "alice@example.org" {
		t.Fatalf("email = %q, want alice@example.org", email)
	}
	if !reflect.DeepEqual(profiles, []string{"ALL"}) {
		t.Fatalf("profiles = %#v, want ALL", profiles)
	}
}
