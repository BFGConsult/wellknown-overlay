package livecheck

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/miekg/dns"
)

func (c Checker) CheckMailAuthDNS(ctx context.Context, domain string, dkimSelectors []string, minTTL uint32) Result {
	result := Result{Name: "MAIL AUTH DNS", Passed: true}
	result.Details = append(result.Details, c.checkSPF(ctx, domain)...)
	result.Details = append(result.Details, c.checkDMARC(ctx, domain)...)
	dkimDetails, dkimProblems := c.checkDKIM(ctx, domain, dkimSelectors)
	result.Details = append(result.Details, dkimDetails...)
	result.Problems = append(result.Problems, dkimProblems...)
	c.warnMailAuthTXTLowTTL(ctx, &result, domain, dkimSelectors, minTTL)
	result.Details = append(result.Details, "DKIM end-to-end message signing: not tested")
	result.Problems = append(result.Problems, "DKIM end-to-end message signing is not tested by this livecheck. Selector DNS records can be correct while outbound messages are still unsigned or signed incorrectly.")
	return result
}

func (c Checker) checkSPF(ctx context.Context, domain string) []string {
	records, err := c.lookupTXT(ctx, domain)
	if err != nil {
		return []string{
			fmt.Sprintf("SPF %s: missing", domain),
			"Suggestion: add exactly one SPF TXT record for " + domain + " covering the actual systems that send mail for this domain.",
		}
	}

	spf := findPrefixedTXT(records, "v=spf1")
	switch len(spf) {
	case 0:
		return []string{
			fmt.Sprintf("SPF %s: missing", domain),
			"Suggestion: add exactly one SPF TXT record for " + domain + " covering the actual systems that send mail for this domain.",
		}
	case 1:
		details := []string{fmt.Sprintf("SPF %s: %s", domain, spf[0])}
		if !spfHasAllMechanism(spf[0]) {
			details = append(details, "SPF warning: record has no all mechanism; consider ending with -all or ~all once all senders are covered.")
		}
		return details
	default:
		sort.Strings(spf)
		return []string{
			fmt.Sprintf("SPF %s: multiple SPF records found: %s", domain, strings.Join(spf, " | ")),
			"Suggestion: merge SPF policy into exactly one TXT record; multiple SPF records make SPF evaluation fail.",
		}
	}
}

func (c Checker) checkDMARC(ctx context.Context, domain string) []string {
	name := "_dmarc." + domain
	records, err := c.lookupTXT(ctx, name)
	if err != nil {
		return []string{
			fmt.Sprintf("DMARC %s: missing", name),
			"Suggestion: add exactly one DMARC TXT record at " + name + " with a p= policy, for example p=none while monitoring or p=quarantine/reject when ready to enforce.",
		}
	}

	dmarc := findPrefixedTXT(records, "v=dmarc1")
	switch len(dmarc) {
	case 0:
		return []string{
			fmt.Sprintf("DMARC %s: missing", name),
			"Suggestion: add exactly one DMARC TXT record at " + name + " with a p= policy, for example p=none while monitoring or p=quarantine/reject when ready to enforce.",
		}
	case 1:
		tags := parseDMARCTags(dmarc[0])
		policy := strings.ToLower(tags["p"])
		if policy == "" {
			return []string{
				fmt.Sprintf("DMARC %s: %s", name, dmarc[0]),
				"DMARC warning: record has no p= policy.",
				"Suggestion: set p=none, p=quarantine, or p=reject.",
			}
		}
		switch policy {
		case "none", "quarantine", "reject":
			return []string{fmt.Sprintf("DMARC %s: p=%s", name, policy)}
		default:
			return []string{
				fmt.Sprintf("DMARC %s: %s", name, dmarc[0]),
				"DMARC warning: invalid p= policy " + policy + ".",
				"Suggestion: set p=none, p=quarantine, or p=reject.",
			}
		}
	default:
		sort.Strings(dmarc)
		return []string{
			fmt.Sprintf("DMARC %s: multiple DMARC records found: %s", name, strings.Join(dmarc, " | ")),
			"Suggestion: merge DMARC policy into exactly one TXT record.",
		}
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

func (c Checker) warnMailAuthTXTLowTTL(ctx context.Context, result *Result, domain string, selectors []string, minTTL uint32) {
	if minTTL == 0 {
		return
	}

	nsRecords, err := c.lookupNS(ctx, domain)
	if err != nil || len(nsRecords) == 0 {
		return
	}

	nameservers := normalizeNSRecords(nsRecords)
	for _, ns := range nameservers {
		c.warnMailAuthTXTRecordLowTTL(ctx, result, ns, domain, "SPF "+domain, minTTL, func(value string) bool {
			return len(findPrefixedTXT([]string{value}, "v=spf1")) > 0
		})

		dmarcName := "_dmarc." + domain
		c.warnMailAuthTXTRecordLowTTL(ctx, result, ns, dmarcName, "DMARC "+dmarcName, minTTL, func(value string) bool {
			return len(findPrefixedTXT([]string{value}, "v=dmarc1")) > 0
		})

		for _, selector := range selectors {
			name := selector + "._domainkey." + domain
			c.warnMailAuthTXTRecordLowTTL(ctx, result, ns, name, "DKIM "+name, minTTL, func(value string) bool {
				return len(findDKIMTXT([]string{value})) > 0
			})
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
