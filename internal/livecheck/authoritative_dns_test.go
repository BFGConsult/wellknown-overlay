package livecheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestCheckAuthoritativeDNSReportsExpectedRecords(t *testing.T) {
	checker := authoritativeDNSChecker(t)
	checker.QueryDNS = func(ctx context.Context, nameserver, name string, qtype uint16) ([]string, error) {
		switch {
		case qtype == dns.TypeSOA:
			return []string{"ns1.example.net. hostmaster.example.org. 2026060601 14400 1800 1209600 86400"}, nil
		case name == "autoconfig.example.org" && qtype == dns.TypeCNAME:
			return []string{"overlay.example.net."}, nil
		case name == "autodiscover.example.org" && qtype == dns.TypeCNAME:
			return []string{"overlay.example.net."}, nil
		case name == "_autodiscover._tcp.example.org" && qtype == dns.TypeSRV:
			return []string{"0 0 443 autodiscover.example.org."}, nil
		case name == "_imaps._tcp.example.org" && qtype == dns.TypeSRV:
			return []string{"0 1 993 mail.example.org."}, nil
		case name == "_submission._tcp.example.org" && qtype == dns.TypeSRV:
			return []string{"0 1 587 mail.example.org."}, nil
		default:
			return nil, errors.New("missing")
		}
	}

	result := checker.CheckAuthoritativeDNS(context.Background(), "alice@example.org", "example.org", 3600)
	if !result.Passed {
		t.Fatalf("authoritative DNS should be advisory-only: %#v", result)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("did not expect problems: %#v", result.Problems)
	}
	for _, want := range []string{
		"authoritative nameservers: ns1.example.net.",
		"SOA serials consistent: 2026060601",
		"autoconfig.example.org @ns1.example.net.: overlay.example.net.",
		"_submission._tcp.example.org @ns1.example.net.: 0 1 587 mail.example.org.",
	} {
		if !containsDetail(result.Details, want) {
			t.Fatalf("expected detail %q in %#v", want, result.Details)
		}
	}
}

func TestCheckAuthoritativeDNSReportsMissingDiscoveryHosts(t *testing.T) {
	checker := authoritativeDNSChecker(t)
	checker.QueryDNS = func(ctx context.Context, nameserver, name string, qtype uint16) ([]string, error) {
		if qtype == dns.TypeSOA {
			return []string{"ns1.example.net. hostmaster.example.org. 2026060601 14400 1800 1209600 86400"}, nil
		}
		return nil, errors.New("missing")
	}

	result := checker.CheckAuthoritativeDNS(context.Background(), "alice@example.org", "example.org", 3600)
	if !result.Passed {
		t.Fatalf("authoritative DNS should be advisory-only: %#v", result)
	}
	for _, want := range []string{
		"autoconfig.example.org is missing from all authoritative nameservers.",
		"Suggestion: add DNS for autodiscover.example.org",
		"Suggestion: add _autodiscover._tcp.example.org. 3600 IN SRV 0 0 443 autodiscover.example.org.",
	} {
		if !containsDetail(result.Problems, want) {
			t.Fatalf("expected problem %q in %#v", want, result.Problems)
		}
	}
}

func TestCheckAuthoritativeDNSReportsUnexpectedSRV(t *testing.T) {
	checker := authoritativeDNSChecker(t)
	checker.QueryDNS = func(ctx context.Context, nameserver, name string, qtype uint16) ([]string, error) {
		switch {
		case qtype == dns.TypeSOA:
			return []string{"ns1.example.net. hostmaster.example.org. 2026060601 14400 1800 1209600 86400"}, nil
		case name == "autoconfig.example.org" && qtype == dns.TypeA:
			return []string{"192.0.2.10"}, nil
		case name == "autodiscover.example.org" && qtype == dns.TypeA:
			return []string{"192.0.2.10"}, nil
		case name == "_autodiscover._tcp.example.org" && qtype == dns.TypeSRV:
			return []string{"5 0 443 example.org."}, nil
		case name == "_imaps._tcp.example.org" && qtype == dns.TypeSRV:
			return []string{"0 1 993 mail.example.org."}, nil
		case name == "_submission._tcp.example.org" && qtype == dns.TypeSRV:
			return []string{"0 1 587 mail.example.org."}, nil
		default:
			return nil, errors.New("missing")
		}
	}

	result := checker.CheckAuthoritativeDNS(context.Background(), "alice@example.org", "example.org", 3600)
	if !result.Passed {
		t.Fatalf("authoritative DNS should be advisory-only: %#v", result)
	}
	if !containsDetail(result.Problems, "_autodiscover._tcp.example.org @ns1.example.net. has unexpected value 5 0 443 example.org.; expected port 443 target autodiscover.example.org.") {
		t.Fatalf("expected unexpected SRV problem in %#v", result.Problems)
	}
}

func TestCheckAuthoritativeDNSReportsLowTTL(t *testing.T) {
	checker := authoritativeDNSChecker(t)
	checker.QueryDNSRecords = func(ctx context.Context, nameserver, name string, qtype uint16) ([]DNSRecord, error) {
		switch {
		case qtype == dns.TypeSOA:
			return []DNSRecord{{Value: "ns1.example.net. hostmaster.example.org. 2026060601 14400 1800 1209600 86400", TTL: 86400}}, nil
		case name == "autoconfig.example.org" && qtype == dns.TypeCNAME:
			return []DNSRecord{{Value: "overlay.example.net.", TTL: 300}}, nil
		case name == "autodiscover.example.org" && qtype == dns.TypeCNAME:
			return []DNSRecord{{Value: "overlay.example.net.", TTL: 3600}}, nil
		case name == "_autodiscover._tcp.example.org" && qtype == dns.TypeSRV:
			return []DNSRecord{{Value: "0 0 443 autodiscover.example.org.", TTL: 3600}}, nil
		case name == "_imaps._tcp.example.org" && qtype == dns.TypeSRV:
			return []DNSRecord{{Value: "0 1 993 mail.example.org.", TTL: 3600}}, nil
		case name == "_submission._tcp.example.org" && qtype == dns.TypeSRV:
			return []DNSRecord{{Value: "0 1 587 mail.example.org.", TTL: 3600}}, nil
		default:
			return nil, errors.New("missing")
		}
	}

	result := checker.CheckAuthoritativeDNS(context.Background(), "alice@example.org", "example.org", 3600)
	if !result.Passed {
		t.Fatalf("authoritative DNS should be advisory-only: %#v", result)
	}
	if !containsDetail(result.Details, "overlay.example.net. (TTL 300)") {
		t.Fatalf("expected TTL detail in %#v", result.Details)
	}
	if !containsDetail(result.Problems, `autoconfig.example.org @ns1.example.net. record "overlay.example.net." has low TTL 300; recommended minimum is 3600`) {
		t.Fatalf("expected low TTL warning in %#v", result.Problems)
	}
}

func TestAuthoritativeDNSWarningsDoNotMakeRunFail(t *testing.T) {
	checker := authoritativeDNSChecker(t)
	checker.QueryDNS = func(ctx context.Context, nameserver, name string, qtype uint16) ([]string, error) {
		if qtype == dns.TypeSOA {
			return []string{"ns1.example.net. hostmaster.example.org. 2026060601 14400 1800 1209600 86400"}, nil
		}
		return nil, errors.New("missing")
	}

	var stdout strings.Builder
	ok, err := runWithChecker(context.Background(), Options{
		EmailAddress:    "alice@example.org",
		Profiles:        []Profile{Thunderbird},
		SkipMailAuthDNS: true,
	}, &stdout, checker)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("authoritative DNS warnings should not fail run:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[AUTHORITATIVE DNS] PASS") || !strings.Contains(stdout.String(), "Summary: PASS (with warnings)") {
		t.Fatalf("expected authoritative DNS warning output:\n%s", stdout.String())
	}
}

func authoritativeDNSChecker(t *testing.T) Checker {
	t.Helper()
	checker := testChecker(map[string]testResponse{
		"https://example.org/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress=alice%40example.org": {
			status: 200,
			body:   thunderbirdXML("example.org"),
		},
	})
	checker.LookupNS = nil
	checker.LookupHost = func(ctx context.Context, host string) ([]string, error) {
		return []string{"192.0.2.10"}, nil
	}
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
	checker.LookupNS = func(ctx context.Context, name string) ([]*net.NS, error) {
		return []*net.NS{{Host: "ns1.example.net."}}, nil
	}
	return checker
}
