package livecheck

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestCheckMailAuthDNSReportsValidSPFAndDMARC(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; p=reject; rua=mailto:dmarc@example.org"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
	if !result.Passed {
		t.Fatalf("mail auth DNS should be advisory-only: %#v", result)
	}
	for _, want := range []string{
		"SPF example.org: v=spf1 mx -all",
		"DMARC _dmarc.example.org: p=reject",
		"DKIM: not tested",
	} {
		if !containsDetail(result.Details, want) {
			t.Fatalf("expected detail %q in %#v", want, result.Details)
		}
	}
	if !containsDetail(result.Problems, "DKIM is not verified") {
		t.Fatalf("expected DKIM warning in %#v", result.Problems)
	}
}

func TestCheckMailAuthDNSReportsMissingSPF(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"_dmarc.example.org": {"v=DMARC1; p=none"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
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

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
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

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
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

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
	if !containsDetail(result.Details, "DMARC _dmarc.example.org: multiple DMARC records found") {
		t.Fatalf("expected multiple DMARC detail in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsDMARCWithoutPolicy(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; rua=mailto:dmarc@example.org"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
	if !containsDetail(result.Details, "DMARC warning: record has no p= policy") {
		t.Fatalf("expected missing p= warning in %#v", result.Details)
	}
}

func TestCheckMailAuthDNSReportsInvalidDMARCPolicy(t *testing.T) {
	checker := testMailAuthChecker(map[string][]string{
		"example.org":        {"v=spf1 mx -all"},
		"_dmarc.example.org": {"v=DMARC1; p=monitor"},
	})

	result := checker.CheckMailAuthDNS(context.Background(), "example.org")
	if !containsDetail(result.Details, "DMARC warning: invalid p= policy monitor") {
		t.Fatalf("expected invalid p= warning in %#v", result.Details)
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
	if strings.Contains(stdout.String(), "with warnings") {
		t.Fatalf("skip should avoid mail-auth warning summary:\n%s", stdout.String())
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
