package overlay

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"strconv"
	"strings"
)

const mailSetupTemplatePath = "templates/mail-setup.md"

//go:embed templates/mail-setup.md translations/*.po
var embeddedMailSetupFS embed.FS

func RenderMailSetup(files fs.FS, cfg MailAccountProfile, manualSetup *MailManualSetupConfig, emailAddress, lang, sharePreviewImageURL string) ([]byte, string, error) {
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

	rendered := renderMailSetupTemplate(template, cfg, emailAddress, lang)
	rendered = appendManualSetupSections(rendered, manualSetup, lang)
	body := markdownDocumentToHTML(rendered, lang, sharePreviewImageURL)
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

func appendManualSetupSections(markdown string, manualSetup *MailManualSetupConfig, lang string) string {
	if manualSetup == nil || len(manualSetup.ExtraSections) == 0 {
		return markdown
	}

	var out strings.Builder
	out.WriteString(strings.TrimRight(markdown, "\n"))
	out.WriteString("\n")
	for _, section := range manualSetup.ExtraSections {
		if !manualSetupSectionMatchesLang(section, lang) {
			continue
		}
		out.WriteString("\n## ")
		out.WriteString(strings.TrimSpace(section.Title))
		out.WriteString("\n\n")
		out.WriteString(strings.TrimSpace(section.BodyMarkdown))
		out.WriteString("\n")
	}
	return out.String()
}

func manualSetupSectionMatchesLang(section MailManualSetupSection, lang string) bool {
	sectionLang := normalizeLanguage(section.Lang)
	if sectionLang == "en" && strings.TrimSpace(section.Lang) == "" {
		return true
	}
	return sectionLang == normalizeLanguage(lang)
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

func markdownDocumentToHTML(markdown, lang, sharePreviewImageURL string) string {
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
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			next := i
			var items []string
			for next < len(lines) {
				nextLine := strings.TrimSpace(lines[next])
				if !strings.HasPrefix(nextLine, "- ") && !strings.HasPrefix(nextLine, "* ") {
					break
				}
				items = append(items, strings.TrimSpace(nextLine[2:]))
				next++
			}
			body.WriteString(markdownListToHTML(items))
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
			body.WriteString(markdownInlineToHTML(strings.Join(paragraph, " ")))
			body.WriteString("</p>\n")
			i = next
		}
	}
	description := mailSetupMetaDescription(lang)
	sharePreviewTags := ""
	if sharePreviewImageURL != "" {
		sharePreviewTags = mailSetupSharePreviewTags(title, description, lang, sharePreviewImageURL)
	}

	return `<!doctype html>
<html lang="` + html.EscapeString(lang) + `">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <meta name="description" content="` + html.EscapeString(description) + `">
` + sharePreviewTags + `  <style>
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

func mailSetupSharePreviewTags(title, description, lang, imageURL string) string {
	locale := openGraphLocale(lang)
	return `  <meta property="og:type" content="website">
  <meta property="og:title" content="` + html.EscapeString(title) + `">
  <meta property="og:description" content="` + html.EscapeString(description) + `">
  <meta property="og:image" content="` + html.EscapeString(imageURL) + `">
  <meta property="og:locale" content="` + html.EscapeString(locale) + `">
  <meta name="twitter:card" content="summary">
  <meta name="twitter:title" content="` + html.EscapeString(title) + `">
  <meta name="twitter:description" content="` + html.EscapeString(description) + `">
  <meta name="twitter:image" content="` + html.EscapeString(imageURL) + `">
`
}

func RenderMailSetupSharePreviewImage() ([]byte, error) {
	const (
		width  = 1200
		height = 630
	)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 247, G: 248, B: 251, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, width, 118), &image.Uniform{C: color.RGBA{R: 24, G: 30, B: 42, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 118, width, 128), &image.Uniform{C: color.RGBA{R: 53, G: 145, B: 255, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(96, 214, 1104, 504), &image.Uniform{C: color.RGBA{R: 255, G: 255, B: 255, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(96, 214, 1104, 220), &image.Uniform{C: color.RGBA{R: 215, G: 220, B: 229, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(96, 498, 1104, 504), &image.Uniform{C: color.RGBA{R: 215, G: 220, B: 229, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(96, 214, 102, 504), &image.Uniform{C: color.RGBA{R: 215, G: 220, B: 229, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(1098, 214, 1104, 504), &image.Uniform{C: color.RGBA{R: 215, G: 220, B: 229, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(160, 278, 1040, 326), &image.Uniform{C: color.RGBA{R: 24, G: 30, B: 42, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(160, 366, 850, 394), &image.Uniform{C: color.RGBA{R: 92, G: 105, B: 124, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(160, 416, 650, 444), &image.Uniform{C: color.RGBA{R: 92, G: 105, B: 124, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(920, 356, 1040, 476), &image.Uniform{C: color.RGBA{R: 53, G: 145, B: 255, A: 255}}, image.Point{}, draw.Src)

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func mailSetupMetaDescription(lang string) string {
	switch normalizeLanguage(lang) {
	case "nb", "nn", "no":
		return "E-postinnstillinger, automatisk oppsett og passordinformasjon."
	default:
		return "Email settings, automatic setup, and password information."
	}
}

func openGraphLocale(lang string) string {
	switch normalizeLanguage(lang) {
	case "nb", "no":
		return "nb_NO"
	case "nn":
		return "nn_NO"
	default:
		return "en_US"
	}
}

func markdownListToHTML(items []string) string {
	if len(items) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<ul>\n")
	for _, item := range items {
		out.WriteString("<li>")
		out.WriteString(markdownInlineToHTML(item))
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n")
	return out.String()
}

func markdownInlineToHTML(value string) string {
	var out strings.Builder
	for {
		before, rest, ok := strings.Cut(value, "[")
		if !ok {
			out.WriteString(html.EscapeString(value))
			break
		}
		label, afterLabel, ok := strings.Cut(rest, "](")
		if !ok {
			out.WriteString(html.EscapeString(before + "[" + rest))
			break
		}
		target, afterTarget, ok := strings.Cut(afterLabel, ")")
		if !ok {
			out.WriteString(html.EscapeString(before + "[" + rest))
			break
		}
		out.WriteString(html.EscapeString(before))
		if safeMarkdownLinkTarget(target) {
			out.WriteString(`<a href="`)
			out.WriteString(html.EscapeString(target))
			out.WriteString(`">`)
			out.WriteString(html.EscapeString(label))
			out.WriteString("</a>")
		} else {
			out.WriteString(html.EscapeString(label))
		}
		value = afterTarget
	}
	return out.String()
}

func safeMarkdownLinkTarget(target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	return strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "/") ||
		strings.HasPrefix(target, "#")
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
