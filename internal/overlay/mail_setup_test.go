package overlay

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRenderMailSetupUsesEnglishTemplateAndEmailAddress(t *testing.T) {
	body, lang, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), nil, "bfg@efn.no", "en")
	if err != nil {
		t.Fatal(err)
	}

	if lang != "en" {
		t.Fatalf("lang = %q, want en", lang)
	}
	got := string(body)
	for _, want := range []string{
		"<h1>Email setup for EFN</h1>",
		`<meta property="og:title" content="Email setup for EFN">`,
		`<meta name="twitter:description" content="Email settings, automatic setup, and password information.">`,
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
	body, _, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), nil, "", "en")
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
	body, lang, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), nil, "", "nb-NO")
	if err != nil {
		t.Fatal(err)
	}

	if lang != "nb" {
		t.Fatalf("lang = %q, want nb", lang)
	}
	got := string(body)
	for _, want := range []string{
		"<h1>E-postoppsett for EFN</h1>",
		`<meta property="og:locale" content="nb_NO">`,
		`<meta property="og:description" content="E-postinnstillinger, automatisk oppsett og passordinformasjon.">`,
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

	body, _, err := RenderMailSetup(files, testMailAccountProfile("default", "EFN"), nil, "", "en")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	if !strings.Contains(got, "<p>Custom EFN login.kristshell.net</p>") {
		t.Fatalf("body = %q", got)
	}
}

func TestRenderMailSetupAppendsExtraSectionsWithMarkdownLinksAndLists(t *testing.T) {
	manualSetup := &MailManualSetupConfig{
		ExtraSections: []MailManualSetupSection{
			{
				Lang:         "en",
				Title:        "Password changes",
				BodyMarkdown: "Change your password in the [KristShell email administration](https://www.kristshell.net/epostadmin/users/login.php).\n\n- Use your full email address as the username.",
			},
		},
	}

	body, _, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), manualSetup, "bfg@efn.no", "en")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	for _, want := range []string{
		"<h2>Password changes</h2>",
		`<a href="https://www.kristshell.net/epostadmin/users/login.php">KristShell email administration</a>`,
		"<li>Use your full email address as the username.</li>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
}

func TestRenderMailSetupFiltersExtraSectionsByLanguage(t *testing.T) {
	manualSetup := &MailManualSetupConfig{
		ExtraSections: []MailManualSetupSection{
			{
				Lang:         "en",
				Title:        "Password changes",
				BodyMarkdown: "Use the password administration page.",
			},
			{
				Lang:         "nb",
				Title:        "Passord",
				BodyMarkdown: "Bruk passordadministrasjonen.",
			},
			{
				Title:        "Support",
				BodyMarkdown: "Contact support if you need help.",
			},
		},
	}

	body, _, err := RenderMailSetup(fstest.MapFS{}, testMailAccountProfile("default", "EFN"), manualSetup, "bfg@efn.no", "nb-NO")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	for _, want := range []string{
		"<h2>Passord</h2>",
		"<h2>Support</h2>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<h2>Password changes</h2>") {
		t.Fatalf("body contains English-only section:\n%s", got)
	}
}
