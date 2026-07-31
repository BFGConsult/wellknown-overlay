package gatewayconfig

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

const (
	orderedRuleVariable = "wellknown_overlay_rule"
	orderedRulePrefix   = "/.wellknown-overlay-internal/rule/"
)

// RenderOrderedRuleMap writes the http-level nginx map that selects the first
// ordered rule matching the normalized request host and path.
func RenderOrderedRuleMap(w writer, cfg overlay.Config) {
	if len(cfg.Rules) == 0 {
		return
	}

	fmt.Fprintf(w, "map \"$host|$uri\" $%s {\n", orderedRuleVariable)
	fmt.Fprintln(w, "    default \"\";")
	// Internal redirects must never re-enter the user rule list.
	fmt.Fprintf(w, "    ~^[^|]*\\|%s.*$ \"\";\n", nginxRegexLiteral(orderedRulePrefix))
	for i, rule := range cfg.Rules {
		fmt.Fprintf(w, "    ~%s %d;\n", orderedRulePattern(rule.Match), i+1)
	}
	fmt.Fprintln(w, "}")
	fmt.Fprintln(w)
}

// RenderOrderedRuleServer writes server-level dispatch and internal action
// locations. Server-level rewrites run before nginx location selection, so an
// ordered rule can intentionally override built-in overlay module routes.
func RenderOrderedRuleServer(w writer, cfg overlay.Config, root string) {
	if len(cfg.Rules) == 0 {
		return
	}

	for i := range cfg.Rules {
		id := i + 1
		fmt.Fprintf(w, "    if ($%s = %d) {\n", orderedRuleVariable, id)
		fmt.Fprintf(w, "        rewrite ^ %s%d last;\n", orderedRulePrefix, id)
		fmt.Fprintln(w, "    }")
	}
	fmt.Fprintln(w)

	for i, rule := range cfg.Rules {
		internalPath := fmt.Sprintf("%s%d", orderedRulePrefix, i+1)
		fmt.Fprintf(w, "    location = %s {\n", nginxQuote(internalPath))
		fmt.Fprintln(w, "        internal;")
		switch {
		case rule.File != "":
			contentType := rule.ContentType
			if contentType == "" {
				contentType = mime.TypeByExtension(filepath.Ext(rule.File))
			}
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			fmt.Fprintln(w, "        types { }")
			fmt.Fprintf(w, "        default_type %s;\n", nginxQuote(contentType))
			fmt.Fprintf(w, "        alias %s;\n", nginxQuote(filepath.Join(root, filepath.FromSlash(rule.File))))
		case rule.Status != 0:
			fmt.Fprintf(w, "        return %d;\n", rule.Status)
		case rule.Redirect != nil:
			origin := strings.TrimSuffix(rule.Redirect.Origin, "/")
			target := nginxQuote(origin)
			if rule.Redirect.PreserveRequestURI {
				target = `"` + nginxEscape(origin) + `$request_uri"`
			}
			fmt.Fprintf(w, "        return %d %s;\n", rule.Redirect.Status, target)
		}
		fmt.Fprintln(w, "    }")
		fmt.Fprintln(w)
	}
}

type writer interface {
	Write([]byte) (int, error)
}

func orderedRulePattern(match overlay.RuleMatch) string {
	if match.All {
		return "^.*$"
	}

	hostPattern := "[^|]*"
	if len(match.Hosts) != 0 {
		hosts := make([]string, 0, len(match.Hosts))
		for _, host := range match.Hosts {
			hosts = append(hosts, nginxRegexLiteral(host))
		}
		hostPattern = "(?:" + strings.Join(hosts, "|") + ")"
	}

	pathPattern := ".*"
	if match.Path != "" {
		pathPattern = nginxRegexLiteral(match.Path)
	} else if match.PathPrefix != "" {
		pathPattern = nginxRegexLiteral(match.PathPrefix) + ".*"
	}
	return "^" + hostPattern + `\|` + pathPattern + "$"
}

// nginxRegexLiteral emits only token-safe ASCII and PCRE byte escapes, so
// user-supplied hosts and paths cannot terminate an nginx map directive.
func nginxRegexLiteral(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' {
			out.WriteByte(b)
			continue
		}
		fmt.Fprintf(&out, `\x%02X`, b)
	}
	return out.String()
}
