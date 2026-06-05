package livecheck

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (c Checker) CheckMailAuthDNS(ctx context.Context, domain string) Result {
	result := Result{Name: "MAIL AUTH DNS", Passed: true}
	result.Details = append(result.Details, c.checkSPF(ctx, domain)...)
	result.Details = append(result.Details, c.checkDMARC(ctx, domain)...)
	result.Details = append(result.Details, "DKIM: not tested (no selector input is currently supported)")
	result.Problems = append(result.Problems, "DKIM is not verified by this livecheck yet. DKIM requires one or more selector names, and this tool currently has no selector input, so assume DKIM still needs separate verification.")
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
