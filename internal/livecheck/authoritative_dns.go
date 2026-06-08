package livecheck

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/miekg/dns"
)

type DNSRecord struct {
	Value string
	TTL   uint32
}

func (c Checker) CheckAuthoritativeDNS(ctx context.Context, email, domain string, minTTL uint32) Result {
	result := Result{Name: "AUTHORITATIVE DNS", Passed: true}

	nsRecords, err := c.lookupNS(ctx, domain)
	if err != nil || len(nsRecords) == 0 {
		result.Details = append(result.Details, "authoritative nameservers: unavailable")
		result.Problems = append(result.Problems, "Could not discover authoritative nameservers for "+domain+": "+errorString(err))
		return result
	}

	nameservers := normalizeNSRecords(nsRecords)
	result.Details = append(result.Details, "authoritative nameservers: "+strings.Join(nameservers, ", "))
	result.Details = append(result.Details, fmt.Sprintf("minimum recommended authoritative DNS TTL: %d", minTTL))

	serials := make(map[string][]string)
	for _, ns := range nameservers {
		records, err := c.queryDNSRecords(ctx, ns, domain, dns.TypeSOA)
		if err != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("SOA %s @%s: %v", domain, ns, err))
			continue
		}
		if len(records) == 0 {
			result.Problems = append(result.Problems, fmt.Sprintf("SOA %s @%s: missing", domain, ns))
			continue
		}
		result.Details = append(result.Details, fmt.Sprintf("SOA %s @%s: %s", domain, ns, formatDNSRecords(records)))
		warnLowTTL(&result, fmt.Sprintf("SOA %s @%s", domain, ns), records, minTTL)
		for _, record := range records {
			if serial := soaSerial(record.Value); serial != "" {
				serials[serial] = append(serials[serial], ns)
			}
		}
	}
	if len(serials) > 1 {
		result.Problems = append(result.Problems, "Authoritative nameservers have inconsistent SOA serials: "+describeSerials(serials))
	} else if len(serials) == 1 {
		for serial := range serials {
			result.Details = append(result.Details, "SOA serials consistent: "+serial)
		}
	}

	settings, err := c.discoverMailSettings(ctx, email, domain)
	if err != nil {
		result.Details = append(result.Details, "could not derive expected authoritative DNS records from Thunderbird Autoconfig: "+err.Error())
		result.Problems = append(result.Problems, "Authoritative DNS checks could not verify expected mail records because Thunderbird Autoconfig was not reachable.")
		return result
	}

	for _, host := range []string{"autoconfig." + domain, "autodiscover." + domain} {
		if ok := c.checkAuthoritativeHost(ctx, &result, nameservers, host, minTTL); !ok {
			result.Problems = append(result.Problems, "Suggestion: add DNS for "+host+" pointing at the host or load balancer serving the overlay.")
		}
	}

	for _, want := range expectedSRVRecords(domain, settings) {
		if ok := c.checkAuthoritativeSRV(ctx, &result, nameservers, want, minTTL); !ok {
			result.Problems = append(result.Problems, fmt.Sprintf("Suggestion: add %s.%s. 3600 IN SRV 0 %d %d %s", want.Service, domain, want.Weight, want.Port, want.Target))
		}
	}

	return result
}

func (c Checker) lookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	if c.LookupNS != nil {
		return c.LookupNS(ctx, name)
	}
	return net.DefaultResolver.LookupNS(ctx, name)
}

func (c Checker) checkAuthoritativeHost(ctx context.Context, result *Result, nameservers []string, host string, minTTL uint32) bool {
	found := false
	for _, ns := range nameservers {
		records := c.authoritativeAnswers(ctx, ns, host, dns.TypeCNAME, dns.TypeA, dns.TypeAAAA)
		if len(records) == 0 {
			result.Details = append(result.Details, fmt.Sprintf("%s @%s: missing", host, ns))
			continue
		}
		found = true
		label := fmt.Sprintf("%s @%s", host, ns)
		result.Details = append(result.Details, fmt.Sprintf("%s: %s", label, formatDNSRecords(records)))
		warnLowTTL(result, label, records, minTTL)
	}
	if !found {
		result.Problems = append(result.Problems, host+" is missing from all authoritative nameservers.")
	}
	return found
}

func (c Checker) checkAuthoritativeSRV(ctx context.Context, result *Result, nameservers []string, want expectedSRV, minTTL uint32) bool {
	name := want.Service + "." + want.Domain
	foundExpected := false
	for _, ns := range nameservers {
		records, err := c.queryDNSRecords(ctx, ns, name, dns.TypeSRV)
		if err != nil || len(records) == 0 {
			result.Details = append(result.Details, fmt.Sprintf("%s @%s: missing (%s)", name, ns, want.Comment))
			continue
		}
		label := fmt.Sprintf("%s @%s", name, ns)
		result.Details = append(result.Details, fmt.Sprintf("%s: %s", label, formatDNSRecords(records)))
		warnLowTTL(result, label, records, minTTL)
		answers := dnsRecordValues(records)
		if srvAnswersContain(answers, want) {
			foundExpected = true
		} else {
			result.Problems = append(result.Problems, fmt.Sprintf("%s @%s has unexpected value %s; expected port %d target %s", name, ns, strings.Join(answers, "; "), want.Port, want.Target))
		}
	}
	if !foundExpected {
		result.Problems = append(result.Problems, name+" is missing expected SRV value from authoritative DNS.")
	}
	return foundExpected
}

func (c Checker) authoritativeAnswers(ctx context.Context, nameserver, name string, qtypes ...uint16) []DNSRecord {
	var answers []DNSRecord
	seen := map[string]struct{}{}
	for _, qtype := range qtypes {
		qanswers, err := c.queryDNSRecords(ctx, nameserver, name, qtype)
		if err != nil {
			continue
		}
		for _, answer := range qanswers {
			if _, ok := seen[answer.Value]; ok {
				continue
			}
			seen[answer.Value] = struct{}{}
			answers = append(answers, answer)
		}
	}
	sort.Slice(answers, func(i, j int) bool { return answers[i].Value < answers[j].Value })
	return answers
}

func (c Checker) queryDNS(ctx context.Context, nameserver, name string, qtype uint16) ([]string, error) {
	records, err := c.queryDNSRecords(ctx, nameserver, name, qtype)
	if err != nil {
		return nil, err
	}
	return dnsRecordValues(records), nil
}

func (c Checker) queryDNSRecords(ctx context.Context, nameserver, name string, qtype uint16) ([]DNSRecord, error) {
	if c.QueryDNSRecords != nil {
		return c.QueryDNSRecords(ctx, nameserver, name, qtype)
	}
	if c.QueryDNS != nil {
		answers, err := c.QueryDNS(ctx, nameserver, name, qtype)
		if err != nil {
			return nil, err
		}
		records := make([]DNSRecord, 0, len(answers))
		for _, answer := range answers {
			records = append(records, DNSRecord{Value: answer})
		}
		return records, nil
	}
	return queryDNSRecords(ctx, nameserver, name, qtype)
}

func queryDNS(ctx context.Context, nameserver, name string, qtype uint16) ([]string, error) {
	records, err := queryDNSRecords(ctx, nameserver, name, qtype)
	if err != nil {
		return nil, err
	}
	return dnsRecordValues(records), nil
}

func queryDNSRecords(ctx context.Context, nameserver, name string, qtype uint16) ([]DNSRecord, error) {
	client := &dns.Client{}
	message := new(dns.Msg)
	message.SetQuestion(dns.Fqdn(name), qtype)
	response, _, err := client.ExchangeContext(ctx, message, net.JoinHostPort(strings.TrimSuffix(nameserver, "."), "53"))
	if err != nil {
		return nil, err
	}
	if response.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("DNS response %s", dns.RcodeToString[response.Rcode])
	}
	var answers []DNSRecord
	for _, rr := range response.Answer {
		answers = append(answers, DNSRecord{
			Value: dnsAnswerString(rr),
			TTL:   rr.Header().Ttl,
		})
	}
	sort.Slice(answers, func(i, j int) bool { return answers[i].Value < answers[j].Value })
	return answers, nil
}

func formatDNSRecords(records []DNSRecord) string {
	values := make([]string, 0, len(records))
	for _, record := range records {
		if record.TTL == 0 {
			values = append(values, record.Value)
			continue
		}
		values = append(values, fmt.Sprintf("%s (TTL %d)", record.Value, record.TTL))
	}
	return strings.Join(values, "; ")
}

func dnsRecordValues(records []DNSRecord) []string {
	values := make([]string, 0, len(records))
	for _, record := range records {
		values = append(values, record.Value)
	}
	sort.Strings(values)
	return values
}

func warnLowTTL(result *Result, label string, records []DNSRecord, minTTL uint32) {
	if minTTL == 0 {
		return
	}
	for _, record := range records {
		if record.TTL == 0 || record.TTL >= minTTL {
			continue
		}
		result.Problems = append(result.Problems, fmt.Sprintf("%s has low TTL %d; recommended minimum is %d once deployment is stable.", label, record.TTL, minTTL))
	}
}

func dnsAnswerString(rr dns.RR) string {
	switch record := rr.(type) {
	case *dns.A:
		return record.A.String()
	case *dns.AAAA:
		return record.AAAA.String()
	case *dns.CNAME:
		return dnsTarget(record.Target)
	case *dns.SRV:
		return fmt.Sprintf("%d %d %d %s", record.Priority, record.Weight, record.Port, dnsTarget(record.Target))
	case *dns.SOA:
		return fmt.Sprintf("%s %s %d %d %d %d %d", dnsTarget(record.Ns), dnsTarget(record.Mbox), record.Serial, record.Refresh, record.Retry, record.Expire, record.Minttl)
	case *dns.TXT:
		return strings.Join(record.Txt, "")
	default:
		return strings.TrimSpace(rr.String())
	}
}

func normalizeNSRecords(records []*net.NS) []string {
	var nameservers []string
	for _, record := range records {
		host := dnsTarget(record.Host)
		nameservers = append(nameservers, host)
	}
	sort.Strings(nameservers)
	return nameservers
}

func soaSerial(answer string) string {
	fields := strings.Fields(answer)
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

func describeSerials(serials map[string][]string) string {
	var parts []string
	for serial, nameservers := range serials {
		sort.Strings(nameservers)
		parts = append(parts, serial+" on "+strings.Join(nameservers, ", "))
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func srvAnswersContain(answers []string, want expectedSRV) bool {
	expected := fmt.Sprintf("0 %d %d %s", want.Weight, want.Port, want.Target)
	for _, answer := range answers {
		if strings.EqualFold(answer, expected) {
			return true
		}
		fields := strings.Fields(answer)
		if len(fields) == 4 && fields[2] == fmt.Sprintf("%d", want.Port) && strings.EqualFold(dnsTarget(fields[3]), want.Target) {
			return true
		}
	}
	return false
}

func errorString(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
