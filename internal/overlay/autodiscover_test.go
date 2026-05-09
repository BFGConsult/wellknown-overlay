package overlay

import (
	"strings"
	"testing"
)

func TestRenderAutodiscoverUsesMailAccountSettings(t *testing.T) {
	body, err := RenderAutodiscover(testMailAccount().SelectProfile("bfg@efn.no"), "bfg@efn.no")
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	wantSubstrings := []string{
		`<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006">`,
		`<Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">`,
		"<DisplayName>EFN</DisplayName>",
		"<EmailAddress>bfg@efn.no</EmailAddress>",
		"<AccountType>email</AccountType>",
		"<Action>settings</Action>",
		"<Type>IMAP</Type>",
		"<Server>login.kristshell.net</Server>",
		"<Port>993</Port>",
		"<LoginName>bfg@efn.no</LoginName>",
		"<SSL>on</SSL>",
		"<Type>SMTP</Type>",
		"<Port>587</Port>",
		"<Encryption>TLS</Encryption>",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Fatalf("body does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<SSL>off</SSL>") {
		t.Fatalf("body contains unnecessary SMTP SSL=off marker:\n%s", got)
	}
}

func TestEmailAddressFromAutodiscoverRequest(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/requestschema/2006">
  <Request>
    <EMailAddress>person+help@efn.no</EMailAddress>
    <AcceptableResponseSchema>http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a</AcceptableResponseSchema>
  </Request>
</Autodiscover>`)

	if got, want := EmailAddressFromAutodiscoverRequest(body), "person+help@efn.no"; got != want {
		t.Fatalf("email = %q, want %q", got, want)
	}
}
