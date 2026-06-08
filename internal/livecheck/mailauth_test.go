package livecheck

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestCheckMailAuthDNSReportsValidSPFAndDMARC(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; p=reject; rua=mailto:dmarc@example.org"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !result.Passed {
		t.Fatalf("mail auth DNS should be advisory-only: %#v", result)
	}
	for _, want := range []string{
		"SPF example.org: v=spf1 mx -all",
		"DMARC _dmarc.example.org: p=reject",
		"DKIM DNS: not tested",
		"DKIM end-to-end message signing: not tested",
	} {
		if !containsDetail(result.Details, want) {
			t.Fatalf("expected detail %q in %#v", want, result.Details)
		}
	}
	if !containsDetail(result.Problems, "DKIM DNS selector records were not tested") {
		t.Fatalf("expected DKIM warning in %#v", result.Problems)
	}
	if !containsDetail(result.Problems, "DKIM end-to-end message signing is not tested") {
		t.Fatalf("expected DKIM end-to-end warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsMissingSPF(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"_dmarc.example.org": {"v=DMARC1; p=none"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !containsDetail(result.Details, "SPF example.org: missing") {
		t.Fatalf("expected missing SPF detail in %#v", result.Details)
	}
	if !containsDetail(result.Details, "covering the actual systems that send mail") {
		t.Fatalf("expected SPF suggestion in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsMultipleSPFRecords(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all", "v=spf1 include:mail.example.org -all"},
		"_dmarc.example.org": {"v=DMARC1; p=none"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !containsDetail(result.Details, "SPF example.org: multiple SPF records found") {
		t.Fatalf("expected multiple SPF detail in %#v", result.Details)
	}
	if !containsDetail(result.Details, "multiple SPF records make SPF evaluation fail") {
		t.Fatalf("expected multiple SPF suggestion in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsMissingDMARC(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org": {"v=spf1 mx -all"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !containsDetail(result.Details, "DMARC _dmarc.example.org: missing") {
		t.Fatalf("expected missing DMARC detail in %#v", result.Details)
	}
	if !containsDetail(result.Details, "add exactly one DMARC TXT record") {
		t.Fatalf("expected DMARC suggestion in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsMultipleDMARCRecords(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; p=none", "v=DMARC1; p=reject"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !containsDetail(result.Details, "DMARC _dmarc.example.org: multiple DMARC records found") {
		t.Fatalf("expected multiple DMARC detail in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsDMARCWithoutPolicy(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; rua=mailto:dmarc@example.org"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !containsDetail(result.Details, "DMARC warning: record has no p= policy") {
		t.Fatalf("expected missing p= warning in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsInvalidDMARCPolicy(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; p=monitor"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 0)
	if !containsDetail(result.Details, "DMARC warning: invalid p= policy monitor") {
		t.Fatalf("expected invalid p= warning in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsValidDKIMSelector(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":                         {"v=spf1 mx -all"},
		"_dmarc.example.org":                  {"v=DMARC1; p=none"},
		"mail2026._domainkey.example.org":     {"v=DKIM1; k=rsa; p=QUJDREVGRw=="},
		"ed25519._domainkey.example.org":      {"v=DKIM1; k=ed25519; p=QUJDREVGRw=="},
		"implicit-rsa._domainkey.example.org": {"v=DKIM1; p=QUJDREVGRw=="},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026", "ed25519", "implicit-rsa"}, 0)
	for _, want := range []string{
		"DKIM mail2026._domainkey.example.org: key type rsa",
		"DKIM ed25519._domainkey.example.org: key type ed25519",
		"DKIM implicit-rsa._domainkey.example.org: key type rsa",
	} {
		if !containsDetail(result.Details, want) {
			t.Fatalf("expected DKIM detail %q in %#v", want, result.Details)
		}
	}
	if containsDetail(result.Problems, "DKIM selector mail2026") {
		t.Fatalf("valid selector should not produce selector warning: %#v", result.Problems)
	}
	if !containsDetail(result.Problems, "DKIM end-to-end message signing is not tested") {
		t.Fatalf("expected end-to-end warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsMissingDKIMSelector(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; p=none"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 0)
	if !containsDetail(result.Details, "DKIM mail2026._domainkey.example.org: missing") {
		t.Fatalf("expected missing DKIM detail in %#v", result.Details)
	}
	if !containsDetail(result.Problems, "add one DKIM TXT record at mail2026._domainkey.example.org") {
		t.Fatalf("expected missing DKIM suggestion in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsMultipleDKIMRecords(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":                     {"v=spf1 mx -all"},
		"_dmarc.example.org":              {"v=DMARC1; p=none"},
		"mail2026._domainkey.example.org": {"v=DKIM1; p=QUJDREVGRw==", "v=DKIM1; p=SElKS0w="},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 0)
	if !containsDetail(result.Details, "DKIM mail2026._domainkey.example.org: multiple DKIM records found") {
		t.Fatalf("expected multiple DKIM detail in %#v", result.Details)
	}
	if !containsDetail(result.Problems, "publish exactly one DKIM TXT record per selector") {
		t.Fatalf("expected multiple DKIM suggestion in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsDKIMMissingPublicKey(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":                     {"v=spf1 mx -all"},
		"_dmarc.example.org":              {"v=DMARC1; p=none"},
		"mail2026._domainkey.example.org": {"v=DKIM1; k=rsa"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 0)
	if !containsDetail(result.Problems, "DKIM selector mail2026: record has no p= public key") {
		t.Fatalf("expected missing p= warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsDKIMEmptyPublicKey(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":                     {"v=spf1 mx -all"},
		"_dmarc.example.org":              {"v=DMARC1; p=none"},
		"mail2026._domainkey.example.org": {"v=DKIM1; p="},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 0)
	if !containsDetail(result.Problems, "DKIM selector mail2026: record has an empty p= public key") {
		t.Fatalf("expected empty p= warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsDKIMMalformedPublicKey(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":                     {"v=spf1 mx -all"},
		"_dmarc.example.org":              {"v=DMARC1; p=none"},
		"mail2026._domainkey.example.org": {"v=DKIM1; p=this is not base64!!!"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 0)
	if !containsDetail(result.Problems, "DKIM selector mail2026: p= public key does not look like valid base64") {
		t.Fatalf("expected malformed p= warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsDKIMMissingVersion(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":                     {"v=spf1 mx -all"},
		"_dmarc.example.org":              {"v=DMARC1; p=none"},
		"mail2026._domainkey.example.org": {"k=rsa; p=QUJDREVGRw=="},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 0)
	if !containsDetail(result.Problems, "DKIM selector mail2026: record does not contain v=DKIM1") {
		t.Fatalf("expected missing v= warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsLowSPFTTL(t *testing.T) {
	checker := testMailAuthCheckerWithAuthoritativeTXT(map[string][]DNSRecord{
		"example.org": {
			{Value: "v=spf1 mx -all", TTL: 300},
			{Value: "unrelated txt", TTL: 300},
		},
		"_dmarc.example.org": {
			{Value: "v=DMARC1; p=none", TTL: 3600},
		},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 3600)
	if !containsDetail(result.Problems, `SPF example.org @ns1.example.org. record "v=spf1 mx -all" has low TTL 300`) {
		t.Fatalf("expected low SPF TTL warning in %#v", result.Problems)
	}
	if containsDetail(result.Problems, "unrelated txt") {
		t.Fatalf("unrelated TXT records should not drive TTL warnings: %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsLowDMARCTTL(t *testing.T) {
	checker := testMailAuthCheckerWithAuthoritativeTXT(map[string][]DNSRecord{
		"example.org": {
			{Value: "v=spf1 mx -all", TTL: 3600},
		},
		"_dmarc.example.org": {
			{Value: "v=DMARC1; p=none", TTL: 300},
		},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", nil, 3600)
	if !containsDetail(result.Problems, `DMARC _dmarc.example.org @ns1.example.org. record "v=DMARC1; p=none" has low TTL 300`) {
		t.Fatalf("expected low DMARC TTL warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsLowDKIMTTL(t *testing.T) {
	checker := testMailAuthCheckerWithAuthoritativeTXT(map[string][]DNSRecord{
		"example.org": {
			{Value: "v=spf1 mx -all", TTL: 3600},
		},
		"_dmarc.example.org": {
			{Value: "v=DMARC1; p=none", TTL: 3600},
		},
		"mail2026._domainkey.example.org": {
			{Value: "v=DKIM1; p=QUJDREVGRw==", TTL: 300},
		},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org", []string{"mail2026"}, 3600)
	if !containsDetail(result.Problems, `DKIM mail2026._domainkey.example.org @ns1.example.org. record "v=DKIM1; p=QUJDREVGRw==" has low TTL 300`) {
		t.Fatalf("expected low DKIM TTL warning in %#v", result.Problems)
	}
}

func TestMailAuthWarningsDoNotFailRun(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: http.StatusOK,
			body:   thunderbirdXML("example.org"),
		},
	})
	var stdout bytes.Buffer

	ok, err := runWithChecker(context.Background(), Options{
		EmailAddress: "alice@example.org",
		Profiles:     []Profile{Thunderbird},
	}, &stdout, checker)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("mail auth warnings should not fail run:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[MAIL AUTH DNS] PASS") {
		t.Fatalf("expected mail auth section:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Summary: PASS (with warnings)") {
		t.Fatalf("expected warning summary:\n%s", stdout.String())
	}
}

func TestSkipMailAuthDNSSuppressesMailAuthSection(t *testing.T) {
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: http.StatusOK,
			body:   thunderbirdXML("example.org"),
		},
	})
	var stdout bytes.Buffer

	ok, err := runWithChecker(context.Background(), Options{
		EmailAddress:    "alice@example.org",
		Profiles:        []Profile{Thunderbird},
		SkipMailAuthDNS: true,
	}, &stdout, checker)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected run to pass:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "[MAIL AUTH DNS]") {
		t.Fatalf("mail auth section should be suppressed:\n%s", stdout.String())
	}
}

func testMailAuthChecker(records map[string][]string) Checker {
	return Checker{
		LookupTXT: func(ctx context.Context, name string) ([]string, error) {
			if txt, ok := records[name]; ok {
				return txt, nil
			}
			return nil, errors.New("no such host")
		},
	}
}

func testMailAuthCheckerWithAuthoritativeTXT(records map[string][]DNSRecord) Checker {
	txtRecords := make(map[string][]string, len(records))
	for name, dnsRecords := range records {
		for _, record := range dnsRecords {
			txtRecords[name] = append(txtRecords[name], record.Value)
		}
	}

	checker := testMailAuthChecker(txtRecords)
	checker.LookupNS = func(ctx context.Context, name string) ([]*net.NS, error) {
		if name == "example.org" {
			return []*net.NS{{Host: "ns1.example.org."}}, nil
		}
		return nil, errors.New("no such host")
	}
	checker.QueryDNSRecords = func(ctx context.Context, ns, name string, qtype uint16) ([]DNSRecord, error) {
		if ns != "ns1.example.org." || qtype != dns.TypeTXT {
			return nil, errors.New("no such host")
		}
		if found, ok := records[name]; ok {
			return found, nil
		}
		return nil, errors.New("no such host")
	}
	return checker
}
