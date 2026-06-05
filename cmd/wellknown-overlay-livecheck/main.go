package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BFGConsult/wellknown-overlay/internal/livecheck"
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
	skipMailAuthDNS := fs.Bool("skip-mail-auth-dns", false, "skip advisory SPF, DMARC, and DKIM DNS checks")
	dkimSelectors := fs.String("dkim-selectors", "", "comma-separated DKIM selectors to check, overrides DKIM_SELECTORS")
	timeout := fs.Duration("timeout", 15*time.Second, "per-request timeout")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() < 1 {
		usage()
		return fmt.Errorf("missing email address")
	}

	profiles, err := livecheck.ParseProfiles(fs.Args()[1:])
	if err != nil {
		return err
	}

	ok, err := livecheck.Run(context.Background(), livecheck.Options{
		EmailAddress:    fs.Arg(0),
		Profiles:        profiles,
		InsecureTLS:     *insecure || *insecureShort,
		Verbose:         *verbose,
		SkipMailAuthDNS: *skipMailAuthDNS,
		DKIMSelectors:   parseDKIMSelectors(*dkimSelectors, os.Getenv("DKIM_SELECTORS")),
		Timeout:         *timeout,
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
  -skip-mail-auth-dns
             skip advisory SPF, DMARC, and DKIM DNS checks
  -dkim-selectors
             comma-separated DKIM selectors to check, overrides DKIM_SELECTORS
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
