package livecheck

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/miekg/dns"
)

type MailAuthCheck string

const (
	MailAuthSPF   MailAuthCheck = "spf"
	MailAuthDMARC MailAuthCheck = "dmarc"
	MailAuthDKIM  MailAuthCheck = "dkim"
	MailAuthAll   MailAuthCheck = "all"
)

func ParseMailAuthChecks(value string) ([]MailAuthCheck, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []MailAuthCheck{MailAuthSPF, MailAuthDMARC, MailAuthDKIM}, nil
	}

	seen := make(map[MailAuthCheck]struct{})
	var checks []MailAuthCheck
	for _, part := range strings.Split(value, ",") {
		check := MailAuthCheck(strings.ToLower(strings.TrimSpace(part)))
		if check == "" {
			continue
		}
		if check == MailAuthAll {
			return []MailAuthCheck{MailAuthSPF, MailAuthDMARC, MailAuthDKIM}, nil
		}
		switch check {
		case MailAuthSPF, MailAuthDMARC, MailAuthDKIM:
			if _, ok := seen[check]; ok {
				continue
			}
			seen[check] = struct{}{}
			checks = append(checks, check)
		default:
			return nil, fmt.Errorf("unknown mail auth check %q", part)
		}
	}
	if len(checks) == 0 {
		return nil, errors.New("no mail auth checks selected")
	}
	return checks, nil
}

func (c Checker) CheckMailAuthDNS(ctx context.Context, domain string, dkimSelectors []string, minTTL uint32) Result {
	return c.CheckMailAuthDNSSelected(ctx, domain, dkimSelectors, minTTL, []MailAuthCheck{MailAuthSPF, MailAuthDMARC, MailAuthDKIM}, false, false)
}

func (c Checker) CheckMailAuthDNSSelected(ctx context.Context, domain string, dkimSelectors []string, minTTL uint32, checks []MailAuthCheck, strict bool, requireDKIMSelectors bool) Result {
	result := Result{Name: "MAIL AUTH DNS", Passed: true}
	if len(checks) == 0 {
		checks = []MailAuthCheck{MailAuthSPF, MailAuthDMARC, MailAuthDKIM}
	}
	selected := selectedMailAuthChecks(checks)

	if selected[MailAuthSPF] {
		details, problems := c.checkSPFStatus(ctx, domain)
		result.Details = append(result.Details, details...)
		if strict {
			result.Problems = append(result.Problems, problems...)
		}
	}
	if selected[MailAuthDMARC] {
		details, problems := c.checkDMARCStatus(ctx, domain)
		result.Details = append(result.Details, details...)
		if strict {
			result.Problems = append(result.Problems, problems...)
		}
	}
	if selected[MailAuthDKIM] {
		dkimDetails, dkimProblems := c.checkDKIM(ctx, domain, dkimSelectors)
		result.Details = append(result.Details, dkimDetails...)
		if strict && len(dkimSelectors) == 0 && !requireDKIMSelectors {
			result.Details = append(result.Details, dkimProblems...)
		} else {
			result.Problems = append(result.Problems, dkimProblems...)
		}
		result.Details = append(result.Details, "DKIM end-to-end message signing: not tested")
		endToEndWarning := "DKIM end-to-end message signing is not tested by this livecheck. Selector DNS records can be correct while outbound messages are still unsigned or signed incorrectly."
		if strict {
			result.Details = append(result.Details, endToEndWarning)
		} else {
			result.Problems = append(result.Problems, endToEndWarning)
		}
	}
	c.warnMailAuthTXTLowTTLSelected(ctx, &result, domain, dkimSelectors, minTTL, selected)
	if len(result.Problems) > 0 && strict {
		result.Passed = false
	}
	return result
}

func (c Checker) checkSPF(ctx context.Context, domain string) []string {
	details, _ := c.checkSPFStatus(ctx, domain)
	return details
}

func (c Checker) checkSPFStatus(ctx context.Context, domain string) ([]string, []string) {
	records, err := c.lookupTXT(ctx, domain)
	if err != nil {
		details := []string{
			fmt.Sprintf("SPF %s: missing", domain),
			"Suggestion: add exactly one SPF TXT record for " + domain + " covering the actual systems that send mail for this domain.",
		}
		return details, details
	}

	spf := findPrefixedTXT(records, "v=spf1")
	switch len(spf) {
	case 0:
		details := []string{
			fmt.Sprintf("SPF %s: missing", domain),
			"Suggestion: add exactly one SPF TXT record for " + domain + " covering the actual systems that send mail for this domain.",
		}
		return details, details
	case 1:
		details := []string{fmt.Sprintf("SPF %s: %s", domain, spf[0])}
		var problems []string
		if !spfHasAllMechanism(spf[0]) {
			warning := "SPF warning: record has no all mechanism; consider ending with -all or ~all once all senders are covered."
			details = append(details, warning)
			problems = append(problems, warning)
		}
		return details, problems
	default:
		sort.Strings(spf)
		details := []string{
			fmt.Sprintf("SPF %s: multiple SPF records found: %s", domain, strings.Join(spf, " | ")),
			"Suggestion: merge SPF policy into exactly one TXT record; multiple SPF records make SPF evaluation fail.",
		}
		return details, details
	}
}

func (c Checker) checkDMARC(ctx context.Context, domain string) []string {
	details, _ := c.checkDMARCStatus(ctx, domain)
	return details
}

func (c Checker) checkDMARCStatus(ctx context.Context, domain string) ([]string, []string) {
	name := "_dmarc." + domain
	records, err := c.lookupTXT(ctx, name)
	if err != nil {
		details := []string{
			fmt.Sprintf("DMARC %s: missing", name),
			"Suggestion: add exactly one DMARC TXT record at " + name + " with a p= policy, for example p=none while monitoring or p=quarantine/reject when ready to enforce.",
		}
		return details, details
	}

	dmarc := findPrefixedTXT(records, "v=dmarc1")
	switch len(dmarc) {
	case 0:
		details := []string{
			fmt.Sprintf("DMARC %s: missing", name),
			"Suggestion: add exactly one DMARC TXT record at " + name + " with a p= policy, for example p=none while monitoring or p=quarantine/reject when ready to enforce.",
		}
		return details, details
	case 1:
		tags := parseDMARCTags(dmarc[0])
		policy := strings.ToLower(tags["p"])
		if policy == "" {
			details := []string{
				fmt.Sprintf("DMARC %s: %s", name, dmarc[0]),
				"DMARC warning: record has no p= policy.",
				"Suggestion: set p=none, p=quarantine, or p=reject.",
			}
			return details, details
		}
		switch policy {
		case "none", "quarantine", "reject":
			return []string{fmt.Sprintf("DMARC %s: p=%s", name, policy)}, nil
		default:
			details := []string{
				fmt.Sprintf("DMARC %s: %s", name, dmarc[0]),
				"DMARC warning: invalid p= policy " + policy + ".",
				"Suggestion: set p=none, p=quarantine, or p=reject.",
			}
			return details, details
		}
	default:
		sort.Strings(dmarc)
		details := []string{
			fmt.Sprintf("DMARC %s: multiple DMARC records found: %s", name, strings.Join(dmarc, " | ")),
			"Suggestion: merge DMARC policy into exactly one TXT record.",
		}
		return details, details
	}
}

func (c Checker) checkDKIM(ctx context.Context, domain string, selectors []string) ([]string, []string) {
	if len(selectors) == 0 {
		return []string{"DKIM DNS: not tested because no selectors were supplied"}, []string{"DKIM DNS selector records were not tested. Supply selectors with -dkim-selectors selector1,selector2 or DKIM_SELECTORS=selector1,selector2."}
	}

	var details []string
	var problems []string
	for _, selector := range selectors {
		name := selector + "._domainkey." + domain
		records, err := c.lookupTXT(ctx, name)
		if err != nil {
			details = append(details, fmt.Sprintf("DKIM %s: missing", name))
			problems = append(problems, "DKIM selector "+selector+" is missing. Suggestion: add one DKIM TXT record at "+name+".")
			continue
		}

		dkim := findDKIMTXT(records)
		switch len(dkim) {
		case 0:
			details = append(details, fmt.Sprintf("DKIM %s: missing", name))
			problems = append(problems, "DKIM selector "+selector+" has TXT records, but none look like DKIM records. Suggestion: add a TXT record containing v=DKIM1 and p=<public-key>.")
			continue
		case 1:
			keyType, warnings := validateDKIMRecord(dkim[0])
			details = append(details, fmt.Sprintf("DKIM %s: key type %s", name, keyType))
			for _, warning := range warnings {
				problems = append(problems, "DKIM selector "+selector+": "+warning)
			}
		default:
			sort.Strings(dkim)
			details = append(details, fmt.Sprintf("DKIM %s: multiple DKIM records found: %s", name, strings.Join(dkim, " | ")))
			problems = append(problems, "DKIM selector "+selector+" has multiple DKIM records. Suggestion: publish exactly one DKIM TXT record per selector.")
		}
	}
	return details, problems
}

func selectedMailAuthChecks(checks []MailAuthCheck) map[MailAuthCheck]bool {
	selected := make(map[MailAuthCheck]bool)
	for _, check := range checks {
		if check == MailAuthAll {
			selected[MailAuthSPF] = true
			selected[MailAuthDMARC] = true
			selected[MailAuthDKIM] = true
			continue
		}
		selected[check] = true
	}
	return selected
}

func (c Checker) warnMailAuthTXTLowTTL(ctx context.Context, result *Result, domain string, selectors []string, minTTL uint32) {
	c.warnMailAuthTXTLowTTLSelected(ctx, result, domain, selectors, minTTL, map[MailAuthCheck]bool{
		MailAuthSPF:   true,
		MailAuthDMARC: true,
		MailAuthDKIM:  true,
	})
}

func (c Checker) warnMailAuthTXTLowTTLSelected(ctx context.Context, result *Result, domain string, selectors []string, minTTL uint32, selected map[MailAuthCheck]bool) {
	if minTTL == 0 {
		return
	}

	nsRecords, err := c.lookupNS(ctx, domain)
	if err != nil || len(nsRecords) == 0 {
		return
	}

	nameservers := normalizeNSRecords(nsRecords)
	for _, ns := range nameservers {
		if selected[MailAuthSPF] {
			c.warnMailAuthTXTRecordLowTTL(ctx, result, ns, domain, "SPF "+domain, minTTL, func(value string) bool {
				return len(findPrefixedTXT([]string{value}, "v=spf1")) > 0
			})
		}

		if selected[MailAuthDMARC] {
			dmarcName := "_dmarc." + domain
			c.warnMailAuthTXTRecordLowTTL(ctx, result, ns, dmarcName, "DMARC "+dmarcName, minTTL, func(value string) bool {
				return len(findPrefixedTXT([]string{value}, "v=dmarc1")) > 0
			})
		}

		if selected[MailAuthDKIM] {
			for _, selector := range selectors {
				name := selector + "._domainkey." + domain
				c.warnMailAuthTXTRecordLowTTL(ctx, result, ns, name, "DKIM "+name, minTTL, func(value string) bool {
					return len(findDKIMTXT([]string{value})) > 0
				})
			}
		}
	}
}

func (c Checker) warnMailAuthTXTRecordLowTTL(ctx context.Context, result *Result, ns, name, label string, minTTL uint32, matches func(string) bool) {
	records, err := c.queryDNSRecords(ctx, ns, name, dns.TypeTXT)
	if err != nil || len(records) == 0 {
		return
	}

	var matched []DNSRecord
	for _, record := range records {
		if matches(record.Value) {
			matched = append(matched, record)
		}
	}
	if len(matched) == 0 {
		return
	}

	warnLowTTL(result, label+" @"+ns, matched, minTTL)
}

func findPrefixedTXT(records []string, prefix string) []string {
	var matches []string
	for _, record := range records {
		trimmed := strings.TrimSpace(record)
		if strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			matches = append(matches, trimmed)
		}
	}
	return matches
}

func findDKIMTXT(records []string) []string {
	var matches []string
	for _, record := range records {
		trimmed := strings.TrimSpace(record)
		tags := parseMailAuthTags(trimmed)
		if strings.Contains(strings.ToLower(trimmed), "v=dkim1") || tags["p"] != "" {
			matches = append(matches, trimmed)
		}
	}
	return matches
}

func spfHasAllMechanism(record string) bool {
	for _, field := range strings.Fields(record) {
		switch strings.ToLower(field) {
		case "-all", "~all", "?all", "+all", "all":
			return true
		}
	}
	return false
}

func parseDMARCTags(record string) map[string]string {
	return parseMailAuthTags(record)
}

func parseMailAuthTags(record string) map[string]string {
	tags := make(map[string]string)
	for _, part := range strings.Split(record, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		tags[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return tags
}

func validateDKIMRecord(record string) (string, []string) {
	tags := parseMailAuthTags(record)
	keyType := strings.ToLower(tags["k"])
	if keyType == "" {
		keyType = "rsa"
	}

	var warnings []string
	if strings.ToLower(tags["v"]) != "dkim1" && !strings.Contains(strings.ToLower(record), "v=dkim1") {
		warnings = append(warnings, "record does not contain v=DKIM1.")
	}

	key, ok := tags["p"]
	if !ok {
		warnings = append(warnings, "record has no p= public key.")
		return keyType, warnings
	}
	key = stripWhitespace(key)
	if key == "" {
		warnings = append(warnings, "record has an empty p= public key.")
		return keyType, warnings
	}
	if !validBase64(key) {
		warnings = append(warnings, "p= public key does not look like valid base64.")
	}
	return keyType, warnings
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func validBase64(value string) bool {
	if _, err := base64.StdEncoding.DecodeString(value); err == nil {
		return true
	}
	_, err := base64.RawStdEncoding.DecodeString(value)
	return err == nil
}
