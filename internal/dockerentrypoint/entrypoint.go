package dockerentrypoint

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

const (
	DefaultConfigPath = "/etc/wellknown-overlay/overlay.json"
	DefaultRootPath   = "/var/lib/wellknown-overlay/public"
	DefaultListenAddr = ":8765"
	OverlayBinary     = "/usr/local/bin/wellknown-overlay"
)

type Execer func(path string, args []string, env []string) error

func Run(args []string, stderr io.Writer, execer Execer) error {
	command := "core"
	commandArgs := []string(nil)
	if len(args) > 1 {
		command = args[1]
		commandArgs = args[2:]
	}

	switch command {
	case "core":
		return RunCore(commandArgs, stderr, execer)
	case "prepare-config":
		return RunPrepareConfig(commandArgs, stderr)
	case "help", "-h", "--help":
		Usage(stderr)
		return nil
	default:
		Usage(stderr)
		return fmt.Errorf("unknown command %q", command)
	}
}

func RunCore(args []string, stderr io.Writer, execer Execer) error {
	fs := flag.NewFlagSet("core", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", EnvOrDefault("OVERLAY_CONFIG", DefaultConfigPath), "configuration file")
	root := fs.String("root", EnvOrDefault("OVERLAY_ROOT", DefaultRootPath), "root directory for route files")
	listen := fs.String("listen", EnvOrDefault("OVERLAY_LISTEN", DefaultListenAddr), "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	WarnGatewayOnlyEnv(stderr)
	if err := PrepareOverlayConfig(*configPath, stderr); err != nil {
		return err
	}

	return execer(OverlayBinary, []string{
		"wellknown-overlay",
		"serve",
		"-config", *configPath,
		"-root", *root,
		"-listen", *listen,
	}, os.Environ())
}

func RunPrepareConfig(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("prepare-config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", EnvOrDefault("OVERLAY_CONFIG", DefaultConfigPath), "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return PrepareOverlayConfig(*configPath, stderr)
}

func Usage(w io.Writer) {
	fmt.Fprintln(w, `usage: wellknown-overlay-docker-entrypoint [command] [options]

commands:
  core            prepare config, then exec the core overlay server
  prepare-config  prepare or validate startup config only`)
}

func PrepareOverlayConfig(configPath string, stderr io.Writer) error {
	if _, err := os.Stat(configPath); err == nil {
		if HasMailEnv() {
			fmt.Fprintf(stderr, "warning: MAIL_* variables ignored because %s exists\n", configPath)
		}
		_, err := overlay.LoadConfig(configPath)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	cfg, err := MailAccountConfigFromEnv()
	if err != nil {
		return fmt.Errorf("%s does not exist and environment config is incomplete: %w", configPath, err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "generated overlay config from MAIL_* variables at %s\n", configPath)
	return nil
}

func MailAccountConfigFromEnv() (overlay.Config, error) {
	if !HasMailEnv() {
		return overlay.Config{}, errors.New("set MAIL_* variables or provide OVERLAY_CONFIG")
	}

	domain, err := RequiredEnv("MAIL_DOMAIN")
	if err != nil {
		return overlay.Config{}, err
	}
	incomingHost, err := RequiredEnv("MAIL_INCOMING_HOST")
	if err != nil {
		return overlay.Config{}, err
	}
	outgoingHost, err := RequiredEnv("MAIL_OUTGOING_HOST")
	if err != nil {
		return overlay.Config{}, err
	}

	incomingPort, err := IntEnv("MAIL_INCOMING_PORT", 993)
	if err != nil {
		return overlay.Config{}, err
	}
	outgoingPort, err := IntEnv("MAIL_OUTGOING_PORT", 587)
	if err != nil {
		return overlay.Config{}, err
	}

	displayName := EnvOrDefault("MAIL_DISPLAY_NAME", domain)
	manualSetup := MailManualSetupConfigFromEnv()
	cfg := overlay.Config{
		MailAccount: &overlay.MailAccount{
			ManualSetup: manualSetup,
			Profiles: []overlay.MailAccountProfile{
				{
					Match:            "default",
					Domain:           domain,
					DisplayName:      displayName,
					DisplayShortName: os.Getenv("MAIL_DISPLAY_SHORT_NAME"),
					Incoming: overlay.EmailServerConfig{
						Type:           EnvOrDefault("MAIL_INCOMING_TYPE", "imap"),
						Hostname:       incomingHost,
						Port:           incomingPort,
						SocketType:     EnvOrDefault("MAIL_INCOMING_SOCKET_TYPE", "SSL"),
						Authentication: EnvOrDefault("MAIL_INCOMING_AUTHENTICATION", "password-cleartext"),
						Username:       EnvOrDefault("MAIL_INCOMING_USERNAME", EnvOrDefault("MAIL_USERNAME", "%EMAILADDRESS%")),
					},
					Outgoing: overlay.EmailServerConfig{
						Type:           EnvOrDefault("MAIL_OUTGOING_TYPE", "smtp"),
						Hostname:       outgoingHost,
						Port:           outgoingPort,
						SocketType:     EnvOrDefault("MAIL_OUTGOING_SOCKET_TYPE", "STARTTLS"),
						Authentication: EnvOrDefault("MAIL_OUTGOING_AUTHENTICATION", "password-cleartext"),
						Username:       EnvOrDefault("MAIL_OUTGOING_USERNAME", EnvOrDefault("MAIL_USERNAME", "%EMAILADDRESS%")),
					},
				},
			},
		},
	}

	return cfg, nil
}

func MailManualSetupConfigFromEnv() *overlay.MailManualSetupConfig {
	manualSetup := overlay.MailManualSetupConfig{
		URL: os.Getenv("MAIL_SETUP_URL"),
	}
	for i := 1; i <= 20; i++ {
		title := os.Getenv(fmt.Sprintf("MAIL_SETUP_EXTRA_SECTION_%d_TITLE", i))
		body := os.Getenv(fmt.Sprintf("MAIL_SETUP_EXTRA_SECTION_%d_BODY_MARKDOWN", i))
		if title == "" && body == "" {
			continue
		}
		manualSetup.ExtraSections = append(manualSetup.ExtraSections, overlay.MailManualSetupSection{
			Title:        title,
			BodyMarkdown: body,
		})
	}
	if manualSetup.URL == "" && len(manualSetup.ExtraSections) == 0 {
		return nil
	}
	return &manualSetup
}

func RequiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func IntEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func EnvOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func HasMailEnv() bool {
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "MAIL_") {
			return true
		}
	}
	return false
}

func WarnGatewayOnlyEnv(stderr io.Writer) {
	for _, name := range GatewayOnlyEnvVars() {
		if os.Getenv(name) != "" {
			fmt.Fprintf(stderr, "warning: %s only affects the gateway image; this is the core image\n", name)
		}
	}
}

func GatewayOnlyEnvVars() []string {
	return []string{
		"BACKEND_URL",
	}
}
