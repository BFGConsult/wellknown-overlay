package livecheck

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestParseProfilesDefaultsToAll(t *testing.T) {
	profiles, err := ParseProfiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := joinProfiles(profiles), "THUNDERBIRD,OUTLOOK,APPLE"; got != want {
		t.Fatalf("profiles = %s, want %s", got, want)
	}
}

func TestParseProfilesAcceptsCommaSeparatedValues(t *testing.T) {
	profiles, err := ParseProfiles([]string{"thunderbird,outlook"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := joinProfiles(profiles), "THUNDERBIRD,OUTLOOK"; got != want {
		t.Fatalf("profiles = %s, want %s", got, want)
	}
}

func TestCheckThunderbirdPassesWhenOneDiscoveryPathWorks(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: http.StatusOK,
			body:   thunderbirdXML("example.org"),
		},
	})

	result := checker.CheckThunderbird(context.Background(), "alice@example.org", "example.org")
	if !result.Passed {
		t.Fatalf("expected Thunderbird check to pass: %#v", result)
	}
	if !containsDetail(result.Details, "DNS autoconfig.example.org: FAIL") {
		t.Fatalf("expected DNS failure to be reported without failing fallback path: %#v", result.Details)
	}
}

func TestWriteResultHidesOptionalFailuresForPassingProfile(t *testing.T) {
	result := Result{
		Name:   string(Thunderbird),
		Passed: true,
		Details: []string{
			"DNS autoconfig.example.org: FAIL: no such host",
			"Suggestion: add DNS for autoconfig.example.org pointing at the host or load balancer serving the overlay.",
			"GET https://example.org/.well-known/autoconfig/mail/config-v1.1.xml: HTTP 200, content-type \"application/xml\"",
			"valid Thunderbird autoconfig XML",
			"one or more optional Thunderbird discovery paths failed; rerun with -v for details",
		},
	}

	var quiet bytes.Buffer
	writeResult(&quiet, result, false)
	if strings.Contains(quiet.String(), "no such host") {
		t.Fatalf("quiet output includes optional failure:\n%s", quiet.String())
	}
	if !strings.Contains(quiet.String(), "rerun with -v") {
		t.Fatalf("quiet output should mention verbose details:\n%s", quiet.String())
	}

	var verbose bytes.Buffer
	writeResult(&verbose, result, true)
	if !strings.Contains(verbose.String(), "no such host") {
		t.Fatalf("verbose output hides optional failure:\n%s", verbose.String())
	}
}

func TestCheckOutlookPassesWhenAutodiscoverEndpointWorks(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/Autodiscover/Autodiscover.xml": {
			status: http.StatusOK,
			body:   autodiscoverXML(),
		},
	})

	result := checker.CheckOutlook(context.Background(), "alice@example.org", "example.org")
	if !result.Passed {
		t.Fatalf("expected Outlook check to pass: %#v", result)
	}
}

func TestCheckAppleFailsWithSuggestionWhenBackendHTMLResponds(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/mail/apple.mobileconfig?emailaddress=alice%40example.org": {
			status:      http.StatusOK,
			contentType: "text/html",
			body:        "<html>backend</html>",
		},
	})

	result := checker.CheckApple(context.Background(), "alice@example.org", "example.org")
	if result.Passed {
		t.Fatalf("expected Apple check to fail: %#v", result)
	}
	if !containsDetail(result.Problems, "reaches the overlay") {
		t.Fatalf("expected routing suggestion: %#v", result.Problems)
	}
}

func TestCheckApplePassesForMobileconfig(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/mail/apple.mobileconfig?emailaddress=alice%40example.org": {
			status: http.StatusOK,
			body: `<?xml version="1.0"?>
<plist version="1.0">
  <dict>
    <key>PayloadType</key>
    <string>com.apple.mail.managed</string>
    <key>EmailAddress</key>
    <string>alice@example.org</string>
  </dict>
</plist>`,
		},
	})

	result := checker.CheckApple(context.Background(), "alice@example.org", "example.org")
	if !result.Passed {
		t.Fatalf("expected Apple check to pass: %#v", result)
	}
}

func TestCheckDNSSRVSuggestsMissingRecords(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: http.StatusOK,
			body:   thunderbirdXML("example.org"),
		},
	})

	result := checker.CheckDNSSRV(context.Background(), "alice@example.org", "example.org")
	if !result.Passed {
		t.Fatalf("DNS SRV warnings should not fail livecheck: %#v", result)
	}
	for _, want := range []string{
		"_imaps._tcp.example.org: missing",
		"_submission._tcp.example.org: missing",
		"_autodiscover._tcp.example.org: missing",
		"_imaps._tcp.example.org. 3600 IN SRV 0 1 993 mail.example.org.",
		"_submission._tcp.example.org. 3600 IN SRV 0 1 587 mail.example.org.",
		"_autodiscover._tcp.example.org. 3600 IN SRV 0 0 443 autodiscover.example.org.",
	} {
		if !containsDetail(result.Details, want) {
			t.Fatalf("expected detail %q in %#v", want, result.Details)
		}
	}
}

func TestCheckDNSSRVReportsExistingRecords(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: http.StatusOK,
			body:   thunderbirdXML("example.org"),
		},
	})
	checker.LookupSRV = func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
		switch service {
		case "autodiscover":
			return "", []*net.SRV{{Target: "autodiscover.example.org.", Port: 443}}, nil
		case "imaps":
			return "", []*net.SRV{{Target: "mail.example.org.", Port: 993}}, nil
		case "submission":
			return "", []*net.SRV{{Target: "mail.example.org.", Port: 587}}, nil
		default:
			return "", nil, errors.New("unexpected SRV lookup")
		}
	}

	result := checker.CheckDNSSRV(context.Background(), "alice@example.org", "example.org")
	if !result.Passed {
		t.Fatalf("DNS SRV check should pass as warning-only: %#v", result)
	}
	if containsDetail(result.Details, "suggested DNS records") {
		t.Fatalf("did not expect suggestions when SRV records exist: %#v", result.Details)
	}
}

func testChecker(responses map[string]testResponse) Checker {
	return Checker{
		HTTPClient: &http.Client{Transport: fakeTransport{responses: responses}},
		LookupHost: func(ctx context.Context, host string) ([]string, error) {
			return nil, errors.New("no such host")
		},
		LookupSRV: func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
			return "", nil, errors.New("no such host")
		},
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			return nil, errors.New("no such host")
		},
	}
}

type testResponse struct {
	status      int
	contentType string
	body        string
}

type fakeTransport struct {
	responses map[string]testResponse
}

func (rt fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, ok := rt.responses[req.URL.String()]
	if !ok {
		return nil, errors.New("unexpected request " + req.URL.String())
	}
	if response.status == 0 {
		response.status = http.StatusOK
	}
	headers := make(http.Header)
	if response.contentType != "" {
		headers.Set("Content-Type", response.contentType)
	}
	return &http.Response{
		StatusCode: response.status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(response.body)),
		Request:    req,
	}, nil
}

func thunderbirdXML(domain string) string {
	return `<?xml version="1.0"?>
<clientConfig version="1.1">
  <emailProvider id="` + domain + `">
    <domain>` + domain + `</domain>
    <incomingServer type="imap">
      <hostname>mail.example.org</hostname>
      <port>993</port>
      <socketType>SSL</socketType>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>mail.example.org</hostname>
      <port>587</port>
      <socketType>STARTTLS</socketType>
    </outgoingServer>
  </emailProvider>
</clientConfig>`
}

func autodiscoverXML() string {
	return `<?xml version="1.0"?>
<Autodiscover>
  <Response>
    <Account>
      <Protocol><Type>IMAP</Type></Protocol>
      <Protocol><Type>SMTP</Type></Protocol>
    </Account>
  </Response>
</Autodiscover>`
}

func joinProfiles(profiles []Profile) string {
	parts := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		parts = append(parts, string(profile))
	}
	return strings.Join(parts, ",")
}

func containsDetail(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
