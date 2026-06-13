package livecheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseThunderbirdSettingsIncludesLoginFields(t *testing.T) {
	settings, err := parseThunderbirdSettings([]byte(roundTripThunderbirdXML("example.org")), "example.org")
	if err != nil {
		t.Fatal(err)
	}
	settings = settings.withEmailAddress("alice@example.org")
	if got, want := settings.Incoming.Username, "alice@example.org"; got != want {
		t.Fatalf("incoming username = %q, want %q", got, want)
	}
	if got, want := settings.Incoming.Authentication, "password-cleartext"; got != want {
		t.Fatalf("incoming auth = %q, want %q", got, want)
	}
	if got, want := settings.Outgoing.Username, "alice@example.org"; got != want {
		t.Fatalf("outgoing username = %q, want %q", got, want)
	}
	if got, want := settings.Outgoing.Authentication, "password-cleartext"; got != want {
		t.Fatalf("outgoing auth = %q, want %q", got, want)
	}
}

func TestReplaceEmailPlaceholderSupportsLocalPart(t *testing.T) {
	if got, want := replaceEmailPlaceholder("%EMAILLOCALPART%", "alice@example.org"), "alice"; got != want {
		t.Fatalf("local placeholder = %q, want %q", got, want)
	}
}

func TestCheckRoundTripPassesWithFakeTransport(t *testing.T) {
	checker := roundTripChecker(t)
	var sent roundTripMessage
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		sent = message
		if server.Username != "alice@example.org" {
			t.Fatalf("SMTP username = %q", server.Username)
		}
		if password != "secret" {
			t.Fatalf("password = %q", password)
		}
		return nil
	}
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		if message.ID != sent.ID {
			t.Fatalf("polled message ID = %q, want %q", message.ID, sent.ID)
		}
		if message.To != "alice@example.org" {
			t.Fatalf("default receiver = %q, want sender", message.To)
		}
		if server.Username != "alice@example.org" {
			t.Fatalf("IMAP username = %q", server.Username)
		}
		if password != "secret" {
			t.Fatalf("receiver password = %q, want sender password", password)
		}
		if keepMessage {
			t.Fatal("keepMessage should be false")
		}
		return roundTripReceipt{
			FoundAfter: 2 * time.Second,
			RawMessage: []byte("From: alice@example.org\r\n" +
				"To: alice@example.org\r\n" +
				"Subject: " + message.Subject + "\r\n" +
				"X-Wellknown-Overlay-Livecheck-ID: " + message.ID + "\r\n\r\nbody"),
			Deleted: true,
		}, nil
	}

	result := checker.CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{
		Enabled:  true,
		Password: "secret",
		Timeout:  30 * time.Second,
	})
	if !result.Passed {
		t.Fatalf("expected pass: %#v", result)
	}
	for _, want := range []string{
		"SMTP sent test message",
		"IMAP found test message after 2s",
		"test message deleted",
		"DKIM end-to-end verification: unsigned message; no DKIM selector found",
	} {
		if !containsDetail(result.Details, want) {
			t.Fatalf("expected detail %q in %#v", want, result.Details)
		}
	}
	if !containsDetail(result.Problems, "no DKIM selector can be inferred") {
		t.Fatalf("expected unsigned DKIM warning in %#v", result.Problems)
	}
}

func TestCheckRoundTripSupportsSeparateReceiver(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: 200,
			body:   roundTripThunderbirdXML("example.org"),
		},
		"https://receiver.example/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=bob%40receiver.example": {
			status: 200,
			body:   roundTripThunderbirdXML("receiver.example"),
		},
	})
	checker.Now = func() time.Time { return time.Unix(1700000000, 123).UTC() }
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		if message.From != "alice@example.org" {
			t.Fatalf("message From = %q", message.From)
		}
		if message.To != "bob@receiver.example" {
			t.Fatalf("message To = %q", message.To)
		}
		if server.Username != "alice@example.org" {
			t.Fatalf("SMTP username = %q", server.Username)
		}
		if password != "sender-secret" {
			t.Fatalf("sender password = %q", password)
		}
		if domain != "example.org" {
			t.Fatalf("sender domain = %q", domain)
		}
		return nil
	}
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		if server.Username != "bob@receiver.example" {
			t.Fatalf("IMAP username = %q", server.Username)
		}
		if password != "receiver-secret" {
			t.Fatalf("receiver password = %q", password)
		}
		if domain != "receiver.example" {
			t.Fatalf("receiver domain = %q", domain)
		}
		return roundTripReceipt{
			FoundAfter: time.Second,
			RawMessage: []byte("From: alice@example.org\r\n" +
				"To: bob@receiver.example\r\n" +
				"X-Wellknown-Overlay-Livecheck-ID: " + message.ID + "\r\n\r\nbody"),
			Deleted: true,
		}, nil
	}

	result := checker.CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{
		Enabled:          true,
		Password:         "sender-secret",
		ReceiverEmail:    "bob@receiver.example",
		ReceiverPassword: "receiver-secret",
	})
	if !result.Passed {
		t.Fatalf("expected pass: %#v", result)
	}
	if !containsDetail(result.Details, "round-trip receiver: bob@receiver.example") {
		t.Fatalf("expected receiver detail in %#v", result.Details)
	}
}

func TestCheckRoundTripSupportsLocalSenderAutoconfig(t *testing.T) {
	dir := t.TempDir()
	senderAutoconfig := filepath.Join(dir, "sender.xml")
	if err := os.WriteFile(senderAutoconfig, []byte(roundTripThunderbirdXML("example.org")), 0o600); err != nil {
		t.Fatal(err)
	}

	checker := testChecker(map[string]testResponse{
		"https://receiver.example/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=bob%40receiver.example": {
			status: 200,
			body:   roundTripThunderbirdXML("receiver.example"),
		},
	})
	checker.Now = func() time.Time { return time.Unix(1700000000, 123).UTC() }
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		if server.Username != "alice@example.org" {
			t.Fatalf("SMTP username = %q", server.Username)
		}
		return nil
	}
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		return roundTripReceipt{
			FoundAfter: time.Second,
			RawMessage: []byte("From: alice@example.org\r\n" +
				"To: bob@receiver.example\r\n" +
				"X-Wellknown-Overlay-Livecheck-ID: " + message.ID + "\r\n\r\nbody"),
			Deleted: true,
		}, nil
	}

	result := checker.CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{
		Enabled:          true,
		Password:         "sender-secret",
		ReceiverEmail:    "bob@receiver.example",
		ReceiverPassword: "receiver-secret",
		SenderAutoconfig: senderAutoconfig,
	})
	if !result.Passed {
		t.Fatalf("expected pass: %#v", result)
	}
}

func TestMailSetupOnlyRunsRoundTripWithoutDiscovery(t *testing.T) {
	dir := t.TempDir()
	senderAutoconfig := filepath.Join(dir, "sender.xml")
	if err := os.WriteFile(senderAutoconfig, []byte(roundTripThunderbirdXML("example.org")), 0o600); err != nil {
		t.Fatal(err)
	}

	checker := testChecker(map[string]testResponse{
		"https://receiver.example/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=bob%40receiver.example": {
			status: 200,
			body:   roundTripThunderbirdXML("receiver.example"),
		},
	})
	checker.Now = func() time.Time { return time.Unix(1700000000, 123).UTC() }
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		return nil
	}
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		return roundTripReceipt{
			FoundAfter: time.Second,
			RawMessage: []byte("From: alice@example.org\r\n" +
				"To: bob@receiver.example\r\n" +
				"X-Wellknown-Overlay-Livecheck-ID: " + message.ID + "\r\n\r\nbody"),
			Deleted: true,
		}, nil
	}

	var stdout strings.Builder
	ok, err := runWithChecker(context.Background(), Options{
		EmailAddress:  "alice@example.org",
		MailSetupOnly: true,
		RoundTrip: RoundTripOptions{
			Enabled:          true,
			Password:         "sender-secret",
			ReceiverEmail:    "bob@receiver.example",
			ReceiverPassword: "receiver-secret",
			SenderAutoconfig: senderAutoconfig,
		},
	}, &stdout, checker)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected mail setup only run to pass:\n%s", stdout.String())
	}
	got := stdout.String()
	if strings.Contains(got, "[THUNDERBIRD]") || strings.Contains(got, "[OUTLOOK]") || strings.Contains(got, "[APPLE]") || strings.Contains(got, "[DNS SRV]") {
		t.Fatalf("mail setup only should not run discovery checks:\n%s", got)
	}
	if !strings.Contains(got, "[ROUND TRIP] PASS") {
		t.Fatalf("mail setup only should run round trip:\n%s", got)
	}
}

func TestMailSetupOnlyRequiresRoundTrip(t *testing.T) {
	var stdout strings.Builder
	_, err := runWithChecker(context.Background(), Options{
		EmailAddress:  "alice@example.org",
		MailSetupOnly: true,
	}, &stdout, roundTripChecker(t))
	if err == nil {
		t.Fatal("expected mail setup only without round trip to fail")
	}
}

func TestCheckRoundTripFailsWithoutPassword(t *testing.T) {
	result := roundTripChecker(t).CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{Enabled: true})
	if result.Passed {
		t.Fatalf("expected failure: %#v", result)
	}
	if !containsDetail(result.Problems, "requires a password") {
		t.Fatalf("expected password problem in %#v", result.Problems)
	}
}

func TestCheckRoundTripFailsWithoutSeparateReceiverPassword(t *testing.T) {
	result := roundTripChecker(t).CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{
		Enabled:       true,
		Password:      "secret",
		ReceiverEmail: "bob@receiver.example",
	})
	if result.Passed {
		t.Fatalf("expected failure: %#v", result)
	}
	if !containsDetail(result.Problems, "LIVECHECK_RECEIVER_PASSWORD") {
		t.Fatalf("expected receiver password problem in %#v", result.Problems)
	}
}

func TestCheckRoundTripReportsSMTPFailure(t *testing.T) {
	checker := roundTripChecker(t)
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		return errors.New("auth failed")
	}

	result := checker.CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{Enabled: true, Password: "secret"})
	if result.Passed {
		t.Fatalf("expected failure: %#v", result)
	}
	if !containsDetail(result.Problems, "SMTP send failed: auth failed") {
		t.Fatalf("expected SMTP problem in %#v", result.Problems)
	}
}

func TestCheckRoundTripReportsIMAPFailure(t *testing.T) {
	checker := roundTripChecker(t)
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		return nil
	}
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		return roundTripReceipt{}, errors.New("timed out")
	}

	result := checker.CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{Enabled: true, Password: "secret"})
	if result.Passed {
		t.Fatalf("expected failure: %#v", result)
	}
	if !containsDetail(result.Problems, "IMAP receive failed: timed out") {
		t.Fatalf("expected IMAP problem in %#v", result.Problems)
	}
}

func TestCheckRoundTripPassesKeepMessageOption(t *testing.T) {
	checker := roundTripChecker(t)
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		return nil
	}
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		if !keepMessage {
			t.Fatal("keepMessage should be true")
		}
		return roundTripReceipt{
			FoundAfter: time.Second,
			RawMessage: []byte("From: alice@example.org\r\n" +
				"X-Wellknown-Overlay-Livecheck-ID: " + message.ID + "\r\n\r\nbody"),
		}, nil
	}

	result := checker.CheckRoundTrip(context.Background(), "alice@example.org", "example.org", RoundTripOptions{
		Enabled:     true,
		Password:    "secret",
		KeepMessage: true,
	})
	if !result.Passed {
		t.Fatalf("expected pass: %#v", result)
	}
	if !containsDetail(result.Details, "test message kept") {
		t.Fatalf("expected kept detail in %#v", result.Details)
	}
}

func TestRoundTripFailureFailsRun(t *testing.T) {
	checker := roundTripChecker(t)
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		return errors.New("auth failed")
	}

	var stdout strings.Builder
	ok, err := runWithChecker(context.Background(), Options{
		EmailAddress: "alice@example.org",
		Profiles:     []Profile{Thunderbird},
		RoundTrip: RoundTripOptions{
			Enabled:  true,
			Password: "secret",
		},
	}, &stdout, checker)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("round-trip failure should fail run:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[ROUND TRIP] FAIL") {
		t.Fatalf("expected round-trip failure output:\n%s", stdout.String())
	}
}

func TestVerifyRoundTripDKIMUnsignedMessageWarns(t *testing.T) {
	details, problems, fatal := Checker{}.verifyRoundTripDKIM([]byte("From: alice@example.org\r\n\r\nbody"), "example.org", nil)
	if !containsDetail(details, "DKIM end-to-end verification: unsigned message; no DKIM selector found") {
		t.Fatalf("expected unsigned detail in %#v", details)
	}
	if !containsDetail(problems, "no DKIM selector can be inferred") {
		t.Fatalf("expected unsigned problem in %#v", problems)
	}
	if fatal {
		t.Fatalf("unsigned DKIM should not be fatal when no selectors are configured")
	}
}

func TestVerifyRoundTripDKIMValidSignaturePasses(t *testing.T) {
	checker := Checker{
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			if name == "brisbane._domainkey.example.com" {
				return []string{dkimFixturePublicKey}, nil
			}
			return nil, errors.New("unexpected lookup " + name)
		},
	}
	details, problems, fatal := checker.verifyRoundTripDKIM([]byte(crlf(dkimFixtureSignedMessage)), "example.com", nil)
	if !containsDetail(details, "DKIM signature d=example.com s=brisbane: pass") {
		t.Fatalf("expected DKIM pass detail in %#v", details)
	}
	if containsDetail(problems, "No passing DKIM signature") {
		t.Fatalf("did not expect missing pass problem in %#v", problems)
	}
	if fatal {
		t.Fatalf("valid DKIM should not be fatal")
	}
}

func TestVerifyRoundTripDKIMValidSignatureMatchesConfiguredSelector(t *testing.T) {
	checker := Checker{
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			if name == "brisbane._domainkey.example.com" {
				return []string{dkimFixturePublicKey}, nil
			}
			return nil, errors.New("unexpected lookup " + name)
		},
	}
	_, problems, fatal := checker.verifyRoundTripDKIM([]byte(crlf(dkimFixtureSignedMessage)), "example.com", []string{"brisbane"})
	if containsDetail(problems, "configured selector") {
		t.Fatalf("did not expect configured selector problem in %#v", problems)
	}
	if fatal {
		t.Fatalf("matching configured selector should not be fatal")
	}
}

func TestVerifyRoundTripDKIMValidSignatureFailsWhenConfiguredSelectorDoesNotMatch(t *testing.T) {
	checker := Checker{
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			if name == "brisbane._domainkey.example.com" {
				return []string{dkimFixturePublicKey}, nil
			}
			return nil, errors.New("unexpected lookup " + name)
		},
	}
	details, problems, fatal := checker.verifyRoundTripDKIM([]byte(crlf(dkimFixtureSignedMessage)), "example.com", []string{"wrong-selector"})
	if !containsDetail(details, "DKIM signature d=example.com s=brisbane: pass") {
		t.Fatalf("expected DKIM pass detail in %#v", details)
	}
	if !containsDetail(problems, "Expected one of: wrong-selector") {
		t.Fatalf("expected configured selector problem in %#v", problems)
	}
	if !fatal {
		t.Fatalf("mismatched configured selector should be fatal")
	}
}

func TestVerifyRoundTripDKIMInvalidSignatureWarns(t *testing.T) {
	checker := Checker{
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			if name == "brisbane._domainkey.example.com" {
				return []string{dkimFixturePublicKey}, nil
			}
			return nil, errors.New("unexpected lookup " + name)
		},
	}
	broken := strings.Replace(dkimFixtureSignedMessage, "Are you hungry yet?", "Are you hungry now?", 1)
	details, problems, fatal := checker.verifyRoundTripDKIM([]byte(crlf(broken)), "example.com", nil)
	if !containsDetail(details, "DKIM signature d=example.com s=brisbane: fail") {
		t.Fatalf("expected DKIM fail detail in %#v", details)
	}
	if !containsDetail(problems, "No passing DKIM signature for example.com") {
		t.Fatalf("expected DKIM failure problem in %#v", problems)
	}
	if fatal {
		t.Fatalf("invalid DKIM should remain advisory when no selectors are configured")
	}
}

func TestBuildRoundTripMessageIncludesUniqueHeader(t *testing.T) {
	message := buildRoundTripMessage("<id@example.org>", "subject", "abc123", "alice@example.org", "bob@example.net", time.Unix(1, 0).UTC())
	if !strings.Contains(string(message), "X-Wellknown-Overlay-Livecheck-ID: abc123\r\n") {
		t.Fatalf("message missing livecheck header:\n%s", string(message))
	}
	if !strings.Contains(string(message), "To: bob@example.net\r\n") {
		t.Fatalf("message missing receiver:\n%s", string(message))
	}
}

func TestExtractDKIMSignatureIdentities(t *testing.T) {
	got := extractDKIMSignatureIdentities([]byte(crlf(dkimFixtureSignedMessage)))
	if len(got) != 1 {
		t.Fatalf("signatures = %#v, want one", got)
	}
	if got[0].Domain != "example.com" || got[0].Selector != "brisbane" {
		t.Fatalf("signature = %#v, want d=example.com s=brisbane", got[0])
	}
}

func roundTripChecker(t *testing.T) Checker {
	t.Helper()
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: 200,
			body:   roundTripThunderbirdXML("example.org"),
		},
	})
	checker.Now = func() time.Time { return time.Unix(1700000000, 123).UTC() }
	checker.PollIMAP = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool, timeout time.Duration, keepMessage bool) (roundTripReceipt, error) {
		return roundTripReceipt{}, errors.New("poll not configured")
	}
	checker.SendMail = func(ctx context.Context, message roundTripMessage, server mailServerSettings, password, domain string, insecureTLS bool) error {
		return errors.New("send not configured")
	}
	return checker
}

func roundTripThunderbirdXML(domain string) string {
	return `<?xml version="1.0"?>
<clientConfig version="1.1">
  <emailProvider id="` + domain + `">
    <domain>` + domain + `</domain>
    <incomingServer type="imap">
      <hostname>imap.example.org</hostname>
      <port>993</port>
      <socketType>SSL</socketType>
      <authentication>password-cleartext</authentication>
      <username>%EMAILADDRESS%</username>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.example.org</hostname>
      <port>587</port>
      <socketType>STARTTLS</socketType>
      <authentication>password-cleartext</authentication>
      <username>%EMAILADDRESS%</username>
    </outgoingServer>
  </emailProvider>
</clientConfig>`
}

func crlf(value string) string {
	return strings.ReplaceAll(value, "\n", "\r\n")
}

const dkimFixturePublicKey = "v=DKIM1; p=MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQ" +
	"KBgQDwIRP/UC3SBsEmGqZ9ZJW3/DkMoGeLnQg1fWn7/zYt" +
	"IxN2SnFCjxOCKG9v3b4jYfcTNh5ijSsq631uBItLa7od+v" +
	"/RtdC2UzJ1lWT947qR+Rcac2gbto/NMqJ0fzfVjH4OuKhi" +
	"tdY9tf6mcwGjaNBcWToIMmPSPDdQPNUYckcQ2QIDAQAB"

const dkimFixtureSignedMessage = `DKIM-Signature: v=1; a=rsa-sha256; s=brisbane; d=example.com;
      c=simple/simple; q=dns/txt; i=joe@football.example.com;
      h=Received : From : To : Subject : Date : Message-ID;
      bh=2jUSOH9NhtVGCQWNr9BrIAPreKQjO6Sn7XIkfJVOzv8=;
      b=AuUoFEfDxTDkHlLXSZEpZj79LICEps6eda7W3deTVFOk4yAUoqOB
      4nujc7YopdG5dWLSdNg6xNAZpOPr+kHxt1IrE+NahM6L/LbvaHut
      KVdkLLkpVaVVQPzeRDI009SO2Il5Lu7rDNH6mZckBdrIx0orEtZV
      4bmp/YzhwvcubU4=;
Received: from client1.football.example.com  [192.0.2.1]
      by submitserver.example.com with SUBMISSION;
      Fri, 11 Jul 2003 21:01:54 -0700 (PDT)
From: Joe SixPack <joe@football.example.com>
To: Suzie Q <suzie@shopping.example.net>
Subject: Is dinner ready?
Date: Fri, 11 Jul 2003 21:00:37 -0700 (PDT)
Message-ID: <20030712040037.46341.5F8J@football.example.com>

Hi.

We lost the game. Are you hungry yet?

Joe.
`
