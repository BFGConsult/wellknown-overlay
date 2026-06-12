package setupnote

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

type Mode string

const (
	ModeMarkdown Mode = "markdown"
	ModeText     Mode = "text"
)

type Options struct {
	Mode     Mode
	Email    string
	Host     string
	Username string
	Password string
	Lang     string
}

func Render(files fs.FS, cfg overlay.Config, opts Options) ([]byte, error) {
	if cfg.MailAccount == nil {
		return nil, errors.New("config has no mail_account")
	}
	mode := opts.Mode
	if mode == "" {
		mode = ModeMarkdown
	}

	profile := cfg.MailAccount.SelectProfileForRequest(opts.Email, opts.Host)
	switch mode {
	case ModeMarkdown:
		return renderMarkdown(files, profile, cfg.MailAccount.ManualSetup, opts)
	case ModeText:
		return renderText(profile, opts), nil
	default:
		return nil, fmt.Errorf("unknown setup note mode %q", mode)
	}
}

func renderMarkdown(files fs.FS, profile overlay.MailAccountProfile, manualSetup *overlay.MailManualSetupConfig, opts Options) ([]byte, error) {
	markdown, _, err := overlay.RenderMailSetupMarkdown(files, profile, manualSetup, overlay.MailSetupRenderOptions{
		EmailAddress:    opts.Email,
		Username:        opts.Username,
		Lang:            opts.Lang,
		PlaceholderMode: overlay.MailSetupLiteralPlaceholders,
	})
	if err != nil {
		return nil, err
	}

	markdown = strings.TrimRight(markdown, "\n") + "\n"
	if opts.Password != "" {
		markdown += "\n## Password\n\nPassword: " + opts.Password + "\n"
	}
	return []byte(markdown), nil
}

func renderText(profile overlay.MailAccountProfile, opts Options) []byte {
	username := opts.Username
	if username == "" {
		username = "%USERNAME%"
	}
	email := opts.Email
	if email == "" {
		email = "%EMAILADDRESS%"
	}

	var out strings.Builder
	writeField(&out, "Display-name", profile.DisplayName)
	writeField(&out, "Email-address", email)
	writeField(&out, "Username", username)
	if opts.Password != "" {
		writeField(&out, "Password", opts.Password)
	}
	writeField(&out, "IMAP-type", profile.Incoming.Type)
	writeField(&out, "IMAP-server", profile.Incoming.Hostname)
	writeField(&out, "IMAP-port", fmt.Sprintf("%d", profile.Incoming.Port))
	writeField(&out, "IMAP-security", profile.Incoming.SocketType)
	writeField(&out, "IMAP-authentication", profile.Incoming.Authentication)
	writeField(&out, "IMAP-username", username)
	writeField(&out, "SMTP-type", profile.Outgoing.Type)
	writeField(&out, "SMTP-server", profile.Outgoing.Hostname)
	writeField(&out, "SMTP-port", fmt.Sprintf("%d", profile.Outgoing.Port))
	writeField(&out, "SMTP-security", profile.Outgoing.SocketType)
	writeField(&out, "SMTP-authentication", profile.Outgoing.Authentication)
	writeField(&out, "SMTP-username", username)
	return []byte(out.String())
}

func writeField(out *strings.Builder, key, value string) {
	out.WriteString(key)
	out.WriteString(": ")
	out.WriteString(value)
	out.WriteString("\n")
}
