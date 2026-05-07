package overlay

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"strconv"
	"strings"
)

const mailSetupTemplatePath = "templates/mail-setup.md"

//go:embed templates/mail-setup.md translations/*.po
var embeddedMailSetupFS embed.FS

func RenderMailSetup(files fs.FS, cfg MailAccountProfile, emailAddress, lang string) ([]byte, string, error) {
	lang = normalizeLanguage(lang)
	template, err := mailSetupTemplate(files)
	if err != nil {
		return nil, "", err
	}
	if lang != "" && lang != "en" {
		translated, err := translatedMailSetupTemplate(files, lang, template)
		if err != nil {
			return nil, "", err
		}
		if translated != "" {
			template = translated
		}
	}

	body := markdownDocumentToHTML(renderMailSetupTemplate(template, cfg, emailAddress, lang), lang)
	return []byte(body), lang, nil
}

func mailSetupTemplate(files fs.FS) (string, error) {
	if body, err := fs.ReadFile(files, "mail-setup.md"); err == nil {
		return string(body), nil
	} else if err != nil && !isNotExist(err) {
		return "", err
	}

	body, err := embeddedMailSetupFS.ReadFile(mailSetupTemplatePath)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func translatedMailSetupTemplate(files fs.FS, lang, source string) (string, error) {
	translationPath := "translations/" + lang + ".po"
	body, err := fs.ReadFile(files, translationPath)
	if err != nil {
		if !isNotExist(err) {
			return "", err
		}
		body, err = embeddedMailSetupFS.ReadFile(translationPath)
		if err != nil {
			if isNotExist(err) {
				return "", nil
			}
			return "", err
		}
	}

	translations, err := parsePOTranslations(string(body))
	if err != nil {
		return "", err
	}
	return translations[source], nil
}

func renderMailSetupTemplate(template string, cfg MailAccountProfile, emailAddress, lang string) string {
	incoming := cfg.Incoming
	outgoing := cfg.Outgoing
	incoming.Username = substituteManualMailVariables(incoming.Username, emailAddress, lang)
	outgoing.Username = substituteManualMailVariables(outgoing.Username, emailAddress, lang)

	values := map[string]string{
		"display_name":            cfg.DisplayName,
		"display_short_name":      cfg.DisplayShortName,
		"domain":                  cfg.Domain,
		"email_address":           manualEmailAddress(emailAddress, lang),
		"incoming.type":           incoming.Type,
		"incoming.hostname":       incoming.Hostname,
		"incoming.port":           fmt.Sprintf("%d", incoming.Port),
		"incoming.socket_type":    incoming.SocketType,
		"incoming.authentication": incoming.Authentication,
		"incoming.username":       incoming.Username,
		"outgoing.type":           outgoing.Type,
		"outgoing.hostname":       outgoing.Hostname,
		"outgoing.port":           fmt.Sprintf("%d", outgoing.Port),
		"outgoing.socket_type":    outgoing.SocketType,
		"outgoing.authentication": outgoing.Authentication,
		"outgoing.username":       outgoing.Username,
	}
	if values["display_short_name"] == "" {
		values["display_short_name"] = cfg.DisplayName
	}

	rendered := template
	for key, value := range values {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}

func substituteManualMailVariables(value, emailAddress, lang string) string {
	if emailAddress != "" {
		return substituteMailVariables(value, emailAddress)
	}
	value = strings.ReplaceAll(value, "%EMAILADDRESS%", manualEmailAddress("", lang))
	value = strings.ReplaceAll(value, "%EMAILLOCALPART%", manualEmailLocalPart(lang))
	return value
}

func manualEmailAddress(emailAddress, lang string) string {
	if emailAddress != "" {
		return emailAddress
	}
	switch normalizeLanguage(lang) {
	case "nb", "nn", "no":
		return "din fulle e-postadresse"
	default:
		return "your full email address"
	}
}

func manualEmailLocalPart(lang string) string {
	switch normalizeLanguage(lang) {
	case "nb", "nn", "no":
		return "delen for @ i e-postadressen din"
	default:
		return "the part before @ in your email address"
	}
}

func normalizeLanguage(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		return "en"
	}
	if before, _, ok := strings.Cut(lang, ","); ok {
		lang = before
	}
	if before, _, ok := strings.Cut(lang, "-"); ok {
		lang = before
	}
	if before, _, ok := strings.Cut(lang, "_"); ok {
		lang = before
	}
	return lang
}

func parsePOTranslations(data string) (map[string]string, error) {
	type poEntry struct {
		msgid  string
		msgstr string
	}

	translations := make(map[string]string)
	var entry poEntry
	var active *string
	flush := func() {
		if entry.msgid != "" && entry.msgstr != "" {
			translations[entry.msgid] = entry.msgstr
		}
		entry = poEntry{}
		active = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "msgid "):
			flush()
			value, err := poQuotedValue(strings.TrimSpace(strings.TrimPrefix(line, "msgid")))
			if err != nil {
				return nil, err
			}
			entry.msgid = value
			active = &entry.msgid
		case strings.HasPrefix(line, "msgstr "):
			value, err := poQuotedValue(strings.TrimSpace(strings.TrimPrefix(line, "msgstr")))
			if err != nil {
				return nil, err
			}
			entry.msgstr = value
			active = &entry.msgstr
		case strings.HasPrefix(line, "\""):
			if active == nil {
				continue
			}
			value, err := poQuotedValue(line)
			if err != nil {
				return nil, err
			}
			*active += value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return translations, nil
}

func markdownDocumentToHTML(markdown, lang string) string {
	var body strings.Builder
	title := "Email setup"
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
			i++
		case strings.HasPrefix(line, "# "):
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			body.WriteString("<h1>")
			body.WriteString(html.EscapeString(title))
			body.WriteString("</h1>\n")
			i++
		case strings.HasPrefix(line, "## "):
			body.WriteString("<h2>")
			body.WriteString(html.EscapeString(strings.TrimSpace(strings.TrimPrefix(line, "## "))))
			body.WriteString("</h2>\n")
			i++
		case strings.HasPrefix(line, "|"):
			next := i
			var tableLines []string
			for next < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[next]), "|") {
				tableLines = append(tableLines, strings.TrimSpace(lines[next]))
				next++
			}
			body.WriteString(markdownTableToHTML(tableLines))
			i = next
		default:
			next := i
			var paragraph []string
			for next < len(lines) {
				nextLine := strings.TrimSpace(lines[next])
				if nextLine == "" || strings.HasPrefix(nextLine, "#") || strings.HasPrefix(nextLine, "|") {
					break
				}
				paragraph = append(paragraph, nextLine)
				next++
			}
			body.WriteString("<p>")
			body.WriteString(html.EscapeString(strings.Join(paragraph, " ")))
			body.WriteString("</p>\n")
			i = next
		}
	}

	return `<!doctype html>
<html lang="` + html.EscapeString(lang) + `">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <style>
    :root { font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #1a1d24; background: #f7f8fb; }
    body { margin: 0; padding: 32px; }
    main { max-width: 760px; margin: 0 auto; padding: 32px; background: #fff; border: 1px solid #d7dce5; border-radius: 8px; box-shadow: 0 16px 40px rgb(19 27 45 / 0.08); }
    h1 { margin: 0 0 16px; font-size: 2rem; line-height: 1.15; }
    h2 { margin: 32px 0 12px; font-size: 1.25rem; }
    p { line-height: 1.6; }
    table { width: 100%; border-collapse: collapse; margin: 12px 0 24px; }
    th, td { text-align: left; padding: 10px 12px; border: 1px solid #d7dce5; vertical-align: top; }
    th { background: #eef1f6; }
  </style>
</head>
<body>
  <main>
` + body.String() + `  </main>
</body>
</html>
`
}

func markdownTableToHTML(lines []string) string {
	if len(lines) < 2 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<table>\n")
	for i, line := range lines {
		if i == 1 && isMarkdownTableSeparator(line) {
			continue
		}
		cells := markdownTableCells(line)
		if len(cells) == 0 {
			continue
		}
		if i == 0 {
			out.WriteString("<thead><tr>")
			for _, cell := range cells {
				out.WriteString("<th>")
				out.WriteString(html.EscapeString(cell))
				out.WriteString("</th>")
			}
			out.WriteString("</tr></thead>\n<tbody>\n")
			continue
		}
		out.WriteString("<tr>")
		for _, cell := range cells {
			out.WriteString("<td>")
			out.WriteString(html.EscapeString(cell))
			out.WriteString("</td>")
		}
		out.WriteString("</tr>\n")
	}
	out.WriteString("</tbody>\n</table>\n")
	return out.String()
}

func markdownTableCells(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func isMarkdownTableSeparator(line string) bool {
	for _, cell := range markdownTableCells(line) {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		for _, ch := range cell {
			if ch != '-' && ch != ':' {
				return false
			}
		}
	}
	return true
}

func poQuotedValue(value string) (string, error) {
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", err
	}
	return unquoted, nil
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
