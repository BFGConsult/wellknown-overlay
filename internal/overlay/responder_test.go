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

func TestResponderDoesNotExposeGatewayTargetedRoute(t *testing.T) {
	responder := NewResponder(Config{
		Routes: []Route{{Path: "/robots.txt", File: "robots.txt", Target: RouteTargetOverlayOnly}},
	}, fstest.MapFS{"robots.txt": {Data: []byte("User-agent: *\n")}})

	_, err := responder.Render("/robots.txt")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestResponderRendersMailAccountRoutes(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccountWithManualSetup(),
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
		`<emailProvider id="example.org">`,
		"<domain>example.org</domain>",
		"<displayName>EFN</displayName>",
		`<incomingServer type="imap">`,
		"<hostname>mail.example.org</hostname>",
		"<port>993</port>",
		"<socketType>SSL</socketType>",
		`<outgoingServer type="smtp">`,
		"<port>587</port>",
		"<socketType>STARTTLS</socketType>",
		"<username>%EMAILADDRESS%</username>",
		`<documentation url="https://autoconfig.example.org/mail/setup">`,
		`<descr lang="en">Manual email setup instructions</descr>`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q:\n%s", want, body)
		}
	}
}

func TestResponderRendersMailSetupRoute(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{})

	response, err := responder.RenderRequest(MailSetupPath, "emailaddress=bfg@example.org&lang=nb")
	if err != nil {
		t.Fatal(err)
	}

	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Status, http.StatusOK)
	}
	if response.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q, want text/html", response.ContentType)
	}
	body := string(response.Body)
	if !strings.Contains(body, "<h1>E-postoppsett for EFN</h1>") || !strings.Contains(body, "bfg@example.org") {
		t.Fatalf("manual setup body does not contain expected content:\n%s", body)
	}
}

func TestResponderMailSetupIgnoresUsernameAndPasswordQueryParameters(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{})

	response, err := responder.RenderRequest(MailSetupPath, "emailaddress=user@example.org&username=bad-user-should-not-render&password=UNIQUE-DO-NOT-RENDER-9d8f7c6b5a4e")
	if err != nil {
		t.Fatal(err)
	}

	body := string(response.Body)
	for _, forbidden := range []string{
		"bad-user-should-not-render",
		"UNIQUE-DO-NOT-RENDER-9d8f7c6b5a4e",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public setup response contains forbidden query value %q:\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, "user@example.org") {
		t.Fatalf("public setup response should still use emailaddress:\n%s", body)
	}
}

func TestResponderRendersAppleMobileconfigRoute(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{})

	response, err := responder.RenderRequest(AppleMobileconfigPath, "emailaddress=bfg@example.org")
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
	if !strings.Contains(body, "<key>EmailAddress</key>") || !strings.Contains(body, "<string>bfg@example.org</string>") {
		t.Fatalf("mobileconfig does not contain substituted email address:\n%s", body)
	}
}

func TestResponderRendersAutodiscoverRoute(t *testing.T) {
	responder := NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{})

	response, err := responder.RenderRequestBody(AutodiscoverPath, "", []byte(`<Autodiscover>
  <Request>
    <EMailAddress>bfg@example.org</EMailAddress>
  </Request>
</Autodiscover>`))
	if err != nil {
		t.Fatal(err)
	}

	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Status, http.StatusOK)
	}
	if response.ContentType != "application/xml" {
		t.Fatalf("content type = %q, want application/xml", response.ContentType)
	}
	body := string(response.Body)
	if !strings.Contains(body, "<LoginName>bfg@example.org</LoginName>") {
		t.Fatalf("autodiscover does not contain substituted login name:\n%s", body)
	}
}

func TestResponderSelectsMailAccountProfileFromEmailAddress(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@example.org", "EFN"),
		testMailAccountProfile("*+help@example.org", "Helpdesk"),
		testMailAccountProfile("default", "Default"),
	}}
	account.Profiles[1].Incoming.Username = "help@example.org"
	account.Profiles[1].Outgoing.Username = "help@example.org"

	responder := NewResponder(Config{MailAccount: &account}, fstest.MapFS{})
	response, err := responder.RenderRequest(ThunderbirdAutoconfigWellKnownPath, "emailaddress=person+help@example.org")
	if err != nil {
		t.Fatal(err)
	}

	body := string(response.Body)
	if !strings.Contains(body, "<displayName>Helpdesk</displayName>") {
		t.Fatalf("body does not use selected profile:\n%s", body)
	}
	if !strings.Contains(body, "<username>help@example.org</username>") {
		t.Fatalf("body does not use selected profile username:\n%s", body)
	}
}

func TestResponderSelectsMailAccountProfileFromHostWhenEmailMissing(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@invest.example.org", "Invest"),
		testMailAccountProfile("*@consult.example.org", "Consult"),
		testMailAccountProfile("default", "Default"),
	}}
	account.Profiles[0].Domain = "invest.example.org"
	account.Profiles[1].Domain = "consult.example.org"

	responder := NewResponder(Config{MailAccount: &account}, fstest.MapFS{})
	response, err := responder.RenderHTTPRequestForHost(ThunderbirdAutoconfigWellKnownPath, "", nil, "", "autoconfig.consult.example.org")
	if err != nil {
		t.Fatal(err)
	}

	body := string(response.Body)
	if !strings.Contains(body, "<displayName>Consult</displayName>") {
		t.Fatalf("body does not use host-selected profile:\n%s", body)
	}
	if !strings.Contains(body, "<domain>consult.example.org</domain>") {
		t.Fatalf("body does not use host-selected domain:\n%s", body)
	}
}

func TestResponderHostFallbackDoesNotOverrideRequestEmail(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@invest.example.org", "Invest"),
		testMailAccountProfile("*@consult.example.org", "Consult"),
		testMailAccountProfile("default", "Default"),
	}}

	responder := NewResponder(Config{MailAccount: &account}, fstest.MapFS{})
	response, err := responder.RenderHTTPRequestForHost(ThunderbirdAutoconfigWellKnownPath, "emailaddress=person%40invest.example.org", nil, "", "autoconfig.consult.example.org")
	if err != nil {
		t.Fatal(err)
	}

	body := string(response.Body)
	if !strings.Contains(body, "<displayName>Invest</displayName>") {
		t.Fatalf("body does not use email-selected profile:\n%s", body)
	}
}

func TestResponderSelectsAutodiscoverProfileFromRequestBody(t *testing.T) {
	account := MailAccount{Profiles: []MailAccountProfile{
		testMailAccountProfile("*@example.org", "EFN"),
		testMailAccountProfile("*+help@example.org", "Helpdesk"),
		testMailAccountProfile("default", "Default"),
	}}
	account.Profiles[1].Incoming.Username = "help@example.org"
	account.Profiles[1].Outgoing.Username = "help@example.org"

	responder := NewResponder(Config{MailAccount: &account}, fstest.MapFS{})
	response, err := responder.RenderRequestBody(AutodiscoverPath, "", []byte(`<Autodiscover>
  <Request>
    <EMailAddress>person+help@example.org</EMailAddress>
  </Request>
</Autodiscover>`))
	if err != nil {
		t.Fatal(err)
	}

	body := string(response.Body)
	if !strings.Contains(body, "<DisplayName>Helpdesk</DisplayName>") {
		t.Fatalf("body does not use selected profile:\n%s", body)
	}
	if !strings.Contains(body, "<LoginName>help@example.org</LoginName>") {
		t.Fatalf("body does not use selected profile username:\n%s", body)
	}
}
