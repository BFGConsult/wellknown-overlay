package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
	"github.com/BFGConsult/wellknown-overlay/internal/setupnote"
)

const (
	envUsername = "WELLKNOWN_OVERLAY_SETUP_NOTE_USERNAME"
	envPassword = "WELLKNOWN_OVERLAY_SETUP_NOTE_PASSWORD"
)

var homeDir = os.UserHomeDir

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("wellknown-overlay-setup-note", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "overlay.json", "configuration file")
	root := fs.String("root", ".", "root directory for setup templates and translations")
	mode := fs.String("mode", string(setupnote.ModeMarkdown), "output mode: markdown or text")
	email := fs.String("email", "", "email address used for profile selection and email placeholder")
	host := fs.String("host", "", "request host used for profile selection when email is omitted")
	username := fs.String("username", "", "username to include in setup instructions")
	password := fs.String("password", "", "optional password to include in setup instructions")
	credentialsPath := fs.String("credentials", "", "optional .netrc-like credentials file, defaults to ~/.netrc when present")
	lang := fs.String("lang", "en", "setup language")
	output := fs.String("output", "", "output file, defaults to stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := overlay.LoadConfig(*configPath)
	if err != nil {
		return err
	}

	explicitCredentials := false
	fs.Visit(func(flag *flag.Flag) {
		if flag.Name == "credentials" {
			explicitCredentials = true
		}
	})
	resolvedUsername, resolvedPassword, err := resolveCredentials(cfg, *email, *host, *username, *password, *credentialsPath, explicitCredentials, os.Getenv)
	if err != nil {
		return err
	}

	body, err := setupnote.Render(os.DirFS(*root), cfg, setupnote.Options{
		Mode:     setupnote.Mode(*mode),
		Email:    *email,
		Host:     *host,
		Username: resolvedUsername,
		Password: resolvedPassword,
		Lang:     *lang,
	})
	if err != nil {
		return err
	}

	if *output == "" {
		_, err = stdout.Write(body)
		return err
	}
	return os.WriteFile(*output, body, 0o600)
}

type getenvFunc func(string) string

type setupCredentials struct {
	Machine  string
	Username string
	Password string
}

func resolveCredentials(cfg overlay.Config, email, host, usernameFlag, passwordFlag, credentialsPath string, explicitCredentials bool, getenv getenvFunc) (string, string, error) {
	username := usernameFlag
	password := passwordFlag

	if username == "" {
		username = getenv(envUsername)
	}
	if password == "" {
		password = getenv(envPassword)
	}
	if username != "" && password != "" {
		return username, password, nil
	}
	if credentialsPath == "" {
		var err error
		credentialsPath, err = defaultCredentialsPath()
		if err != nil || credentialsPath == "" {
			return username, password, nil
		}
	}

	credentials, err := readCredentialsFile(credentialsPath)
	if err != nil {
		if !explicitCredentials && errors.Is(err, os.ErrNotExist) {
			return username, password, nil
		}
		return "", "", err
	}
	selected, err := selectCredentials(credentials, credentialSelectors(cfg, email, host))
	if err != nil {
		return "", "", err
	}
	if username == "" {
		username = selected.Username
	}
	if password == "" {
		password = selected.Password
	}
	return username, password, nil
}

func defaultCredentialsPath() (string, error) {
	home, err := homeDir()
	if err != nil || home == "" {
		return "", err
	}
	return home + string(os.PathSeparator) + ".netrc", nil
}

func readCredentialsFile(filename string) ([]setupCredentials, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	credentials, err := parseCredentials(string(data))
	if err != nil {
		return nil, err
	}
	return credentials, nil
}

func parseCredentials(data string) ([]setupCredentials, error) {
	tokens := credentialTokens(data)
	var credentials []setupCredentials
	current := setupCredentials{}
	seenField := false
	flush := func() {
		if !seenField {
			return
		}
		credentials = append(credentials, current)
		current = setupCredentials{}
		seenField = false
	}

	for i := 0; i < len(tokens); i++ {
		token := strings.ToLower(tokens[i])
		switch token {
		case "machine":
			flush()
			i++
			if i >= len(tokens) {
				return nil, errors.New("credentials file: machine requires a value")
			}
			current.Machine = normalizeCredentialName(tokens[i])
			seenField = true
		case "login", "username":
			i++
			if i >= len(tokens) {
				return nil, errors.New("credentials file: login requires a value")
			}
			current.Username = tokens[i]
			seenField = true
		case "password":
			i++
			if i >= len(tokens) {
				return nil, errors.New("credentials file: password requires a value")
			}
			current.Password = tokens[i]
			seenField = true
		default:
			return nil, fmt.Errorf("credentials file: unknown token %q", tokens[i])
		}
	}
	flush()
	return credentials, nil
}

func credentialTokens(data string) []string {
	var clean []string
	for _, line := range strings.Split(data, "\n") {
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = before
		}
		clean = append(clean, line)
	}
	return strings.Fields(strings.Join(clean, "\n"))
}

func selectCredentials(credentials []setupCredentials, selectors []string) (setupCredentials, error) {
	complete := make([]setupCredentials, 0, len(credentials))
	for _, credential := range credentials {
		if credential.Username != "" && credential.Password != "" {
			complete = append(complete, credential)
		}
	}
	if len(complete) == 0 {
		return setupCredentials{}, errors.New("credentials file has no complete login/password entry")
	}
	if len(complete) == 1 {
		return complete[0], nil
	}

	selectorSet := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		selector = normalizeCredentialName(selector)
		if selector != "" {
			selectorSet[selector] = struct{}{}
		}
	}
	for _, credential := range complete {
		if _, ok := selectorSet[credential.Machine]; ok && credential.Machine != "" {
			return credential, nil
		}
	}
	return setupCredentials{}, errors.New("credentials file has multiple entries; add -email or -host matching a machine entry")
}

func credentialSelectors(cfg overlay.Config, email, host string) []string {
	var selectors []string
	if host != "" {
		selectors = append(selectors, host)
	}
	if domain := domainFromEmail(email); domain != "" {
		selectors = append(selectors, domain)
	}
	if cfg.MailAccount != nil {
		profile := cfg.MailAccount.SelectProfileForRequest(email, host)
		selectors = append(selectors, profile.Domain)
	}

	var expanded []string
	for _, selector := range selectors {
		selector = normalizeCredentialName(selector)
		if selector == "" {
			continue
		}
		expanded = append(expanded, selector)
		for _, prefix := range []string{"autoconfig.", "autodiscover."} {
			if strings.HasPrefix(selector, prefix) {
				expanded = append(expanded, strings.TrimPrefix(selector, prefix))
			}
		}
	}
	return expanded
}

func domainFromEmail(email string) string {
	_, domain, ok := strings.Cut(strings.TrimSpace(email), "@")
	if !ok {
		return ""
	}
	return domain
}

func normalizeCredentialName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, ".")
	if host, _, ok := strings.Cut(value, ":"); ok {
		value = host
	}
	return value
}
