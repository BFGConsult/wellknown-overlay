package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BFGConsult/wellknown-overlay/internal/livecheck"
	"golang.org/x/term"
)

var (
	stdinIsTerminal = term.IsTerminal
	readPassword    = term.ReadPassword
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("wellknown-overlay-livecheck", flag.ContinueOnError)
	insecure := fs.Bool("insecure", false, "skip TLS certificate verification")
	insecureShort := fs.Bool("k", false, "skip TLS certificate verification")
	verbose := fs.Bool("v", false, "show optional failed discovery attempts even when a profile passes")
	configPath := fs.String("livecheck-config", "", "local KEY=value config file for livecheck secrets/options")
	skipMailAuthDNS := fs.Bool("skip-mail-auth-dns", false, "skip advisory SPF, DMARC, and DKIM DNS checks")
	minDNSTTLFlag := fs.Int("min-dns-ttl", -1, "minimum recommended authoritative DNS TTL in seconds")
	dkimSelectors := fs.String("dkim-selectors", "", "comma-separated DKIM selectors to check, overrides DKIM_SELECTORS")
	mailSetupOnly := fs.Bool("mail-setup-only", false, "only run credentialed IMAP/SMTP round-trip setup testing")
	roundTrip := fs.Bool("round-trip", false, "send a test message over SMTP and verify receipt over IMAP")
	roundTripTimeout := fs.Duration("round-trip-timeout", 60*time.Second, "total timeout for the round-trip test")
	roundTripKeepMessage := fs.Bool("round-trip-keep-message", false, "keep the round-trip test message instead of deleting it")
	timeout := fs.Duration("timeout", 15*time.Second, "per-request timeout")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	fileConfig, err := readLivecheckConfig(*configPath)
	if err != nil {
		return err
	}
	emailAddress, profileArgs := resolveEmailAndProfileArgs(fs.Args(), fileConfig)
	if strings.TrimSpace(emailAddress) == "" {
		usage()
		return fmt.Errorf("missing email address; pass one as an argument or set LIVECHECK_EMAIL in -livecheck-config")
	}

	profiles, err := livecheck.ParseProfiles(profileArgs)
	if err != nil {
		return err
	}
	password, err := roundTripPassword(*roundTrip, configValue("LIVECHECK_PASSWORD", fileConfig))
	if err != nil {
		return err
	}
	minDNSTTL, err := minDNSTTL(*minDNSTTLFlag, configValue("LIVECHECK_MIN_DNS_TTL", fileConfig))
	if err != nil {
		return err
	}
	parsedDKIMSelectors := parseDKIMSelectors(*dkimSelectors, configValue("DKIM_SELECTORS", fileConfig))

	ok, err := livecheck.Run(context.Background(), livecheck.Options{
		EmailAddress:    emailAddress,
		Profiles:        profiles,
		InsecureTLS:     *insecure || *insecureShort,
		Verbose:         *verbose,
		SkipMailAuthDNS: *skipMailAuthDNS,
		MailSetupOnly:   *mailSetupOnly,
		DKIMSelectors:   parsedDKIMSelectors,
		MinDNSTTL:       minDNSTTL,
		RoundTrip: livecheck.RoundTripOptions{
			Enabled:          *roundTrip,
			Password:         password,
			ReceiverEmail:    configValue("LIVECHECK_RECEIVER_EMAIL", fileConfig),
			ReceiverPassword: configValue("LIVECHECK_RECEIVER_PASSWORD", fileConfig),
			SenderAutoconfig: configValue("LIVECHECK_SENDER_AUTOCONFIG", fileConfig),
			DKIMSelectors:    parsedDKIMSelectors,
			Timeout:          *roundTripTimeout,
			KeepMessage:      *roundTripKeepMessage,
			InsecureTLS:      *insecure || *insecureShort,
		},
		Timeout: *timeout,
	}, os.Stdout)
	if err != nil {
		return err
	}
	if !ok {
		os.Exit(1)
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: wellknown-overlay-livecheck [options] email@example.org [THUNDERBIRD,OUTLOOK,APPLE|ALL]

options:
  -insecure  skip TLS certificate verification for diagnosis
  -k         alias for -insecure
  -v         show optional failed discovery attempts even when a profile passes
  -livecheck-config
             local KEY=value config file; supports LIVECHECK_EMAIL, LIVECHECK_PASSWORD,
             LIVECHECK_RECEIVER_EMAIL, LIVECHECK_RECEIVER_PASSWORD,
             LIVECHECK_SENDER_AUTOCONFIG, LIVECHECK_MIN_DNS_TTL, and DKIM_SELECTORS
  -skip-mail-auth-dns
             skip advisory SPF, DMARC, and DKIM DNS checks
  -min-dns-ttl
             warn when authoritative DNS TTL is below this value, default 3600
  -dkim-selectors
             comma-separated DKIM selectors to check, overrides DKIM_SELECTORS
  -mail-setup-only
             only run credentialed IMAP/SMTP setup testing; requires -round-trip
  -round-trip
             send a test message over SMTP and verify receipt over IMAP
  -round-trip-timeout
             total timeout for the round-trip test, default 60s
  -round-trip-keep-message
             keep the round-trip test message instead of deleting it
  -timeout   per-request timeout, default 15s`)
}

func parseDKIMSelectors(cliValue, envValue string) []string {
	value := strings.TrimSpace(cliValue)
	if value == "" {
		value = strings.TrimSpace(envValue)
	}
	if value == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var selectors []string
	for _, part := range strings.Split(value, ",") {
		selector := strings.TrimSpace(part)
		if selector == "" {
			continue
		}
		if _, ok := seen[selector]; ok {
			continue
		}
		seen[selector] = struct{}{}
		selectors = append(selectors, selector)
	}
	return selectors
}

func minDNSTTL(cliValue int, envValue string) (uint32, error) {
	if cliValue >= 0 {
		return uint32(cliValue), nil
	}
	value := strings.TrimSpace(envValue)
	if value == "" {
		return 3600, nil
	}
	ttl, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LIVECHECK_MIN_DNS_TTL %q: %w", value, err)
	}
	return uint32(ttl), nil
}

func readLivecheckConfig(path string) (map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=value", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s:%d: empty key", path, lineNumber)
		}
		config[key] = trimConfigValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return config, nil
}

func trimConfigValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func configValue(key string, fileConfig map[string]string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fileConfig[key]
}

func resolveEmailAndProfileArgs(args []string, fileConfig map[string]string) (string, []string) {
	emailAddress := configValue("LIVECHECK_EMAIL", fileConfig)
	if len(args) == 0 {
		return emailAddress, nil
	}
	if strings.TrimSpace(emailAddress) != "" && !strings.Contains(args[0], "@") {
		return emailAddress, args
	}
	return args[0], args[1:]
}

func roundTripPassword(enabled bool, envValue string) (string, error) {
	if !enabled {
		return "", nil
	}
	if strings.TrimSpace(envValue) != "" {
		return envValue, nil
	}
	fd := int(os.Stdin.Fd())
	if !stdinIsTerminal(fd) {
		return "", fmt.Errorf("round-trip test requires a password; set LIVECHECK_PASSWORD or run interactively")
	}
	fmt.Fprint(os.Stderr, "Mail password: ")
	password, err := readPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if len(password) == 0 {
		return "", fmt.Errorf("round-trip test requires a non-empty password")
	}
	return string(password), nil
}
