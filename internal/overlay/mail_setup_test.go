package overlay

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRenderMailSetupUsesEnglishTemplateAndEmailAddress(t *testing.T) {
	body, lang, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), "bfg@efn.no", "en")
	if err != nil {
		t.Fatal(err)
	}

	if lang != "en" {
		t.Fatalf("lang = %q, want en", lang)
	}
	got := string(body)
	for _, want := range []string{
		"<h1>Email setup for EFN</h1>",
		"Your email address: bfg@efn.no",
		"<td>Server name</td><td>login.kristshell.net</td>",
		"<td>Username</td><td>bfg@efn.no</td>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderMailSetupUsesHumanPlaceholdersWithoutEmailAddress(t *testing.T) {
	body, _, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), "", "en")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	if !strings.Contains(got, "Your email address: your full email address") {
		t.Fatalf("body does not contain human email placeholder:\n%s", got)
	}
	if !strings.Contains(got, "<td>Username</td><td>your full email address</td>") {
		t.Fatalf("body does not contain human username placeholder:\n%s", got)
	}
}

func TestRenderMailSetupUsesNorwegianPOTranslation(t *testing.T) {
	body, lang, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), "", "nb-NO")
	if err != nil {
		t.Fatal(err)
	}

	if lang != "nb" {
		t.Fatalf("lang = %q, want nb", lang)
	}
	got := string(body)
	for _, want := range []string{
		"<h1>E-postoppsett for EFN</h1>",
		"Din e-postadresse: din fulle e-postadresse",
		"<td>Servernavn</td><td>login.kristshell.net</td>",
		"<td>Brukernavn</td><td>din fulle e-postadresse</td>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderMailSetupAllowsRootTemplateOverride(t *testing.T) {
	files := fstest.MapFS{
		"mail-setup.md": {Data: []byte("Custom {{display_name}} {{incoming.hostname}}\n")},
	}

	body, _, err := RenderMailSetup(files, testMailAccountProfile("default", "EFN"), "", "en")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	if !strings.Contains(got, "<p>Custom EFN login.kristshell.net</p>") {
		t.Fatalf("body = %q", got)
	}
}
