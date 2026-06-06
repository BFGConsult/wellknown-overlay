# wellknown-overlay

A tiny standards-path overlay for existing domains.

`wellknown-overlay` is intended for domains where another application owns the
site, but you still need to serve small standards-owned paths such as
`/.well-known/autoconfig/...`, `/Autodiscover/Autodiscover.xml`, or future
modules such as WKD for OpenPGP.

The project starts from the idea behind `mail-account-php`: keep one source
of truth, render the protocol-specific outputs, and avoid turning a handful of
well-known routes into a full backend application.

## Shape

The overlay owns only explicitly configured routes. Everything else should be
left to the main site by the reverse proxy, or return `404` when the overlay is
used directly.

```text
request
  -> reverse proxy
      overlay-owned route -> wellknown-overlay
      all other routes    -> existing app
```

The initial implementation supports static route overlays and an email
autoconfig module. Future first-class modules may include:

- OpenPGP WKD
- `security.txt`
- MTA-STS
- WebFinger

## Configuration

Example:

```json
{
  "routes": [
    {
      "path": "/.well-known/security.txt",
      "file": "security.txt",
      "content_type": "text/plain; charset=utf-8"
    },
    {
      "path": "/.well-known/autoconfig/mail/config-v1.1.xml",
      "file": "mail/config-v1.1.xml",
      "content_type": "application/xml"
    }
  ]
}
```

Route paths must be absolute and are matched exactly.

### Mail Account

The `mail_account` module describes one mail account setup as structured data.
Renderers serve client-specific configuration formats from that source of
truth:

```json
{
  "mail_account": {
    "manual_setup": {
      "url": "https://autoconfig.example.org/mail/setup",
      "share_preview": {
        "enabled": true
      },
      "extra_sections": [
        {
          "lang": "en",
          "title": "Password changes",
          "body_markdown": "Change your password in the [KristShell email administration](https://www.kristshell.net/epostadmin/users/login.php)."
        }
      ]
    },
    "profiles": [
      {
        "match": "*+bfg@example.org",
        "domain": "example.org",
        "display_name": "Example Mail",
        "incoming": {
          "type": "imap",
          "hostname": "mail.example.org",
          "port": 993,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "bfg@example.org"
        },
        "outgoing": {
          "type": "smtp",
          "hostname": "mail.example.org",
          "port": 587,
          "socket_type": "STARTTLS",
          "authentication": "password-cleartext",
          "username": "bfg@example.org"
        }
      },
      {
        "match": "default",
        "domain": "example.org",
        "display_name": "Example Mail",
        "incoming": {
          "type": "imap",
          "hostname": "mail.example.org",
          "port": 993,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        },
        "outgoing": {
          "type": "smtp",
          "hostname": "mail.example.org",
          "port": 587,
          "socket_type": "STARTTLS",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        }
      }
    ]
  }
}
```

Profile matching first uses the email address supplied by the client, for
example the `emailaddress` query parameter or the Outlook Autodiscover request
body. The profile with `match: "default"` is required and is used when no other
profile matches. All other `match` values are case-insensitive glob patterns
against the full email address; `*` matches any sequence and `?` matches one
character. More literal characters beat fewer literal characters, fewer
wildcards break that tie, and config order breaks any remaining tie.

If the client does not supply an email address, the overlay falls back to the
HTTP `Host` value. It tries the host as a mail domain, and also strips the
standard discovery prefixes `autoconfig.` and `autodiscover.` before matching.
For example, `Host: autoconfig.example.org` can select a profile with
`match: "*@example.org"`. When both an email address and `Host` are available,
the email address wins.

When enabled, the module owns these exact Thunderbird routes:

- `/.well-known/autoconfig/mail/config-v1.1.xml`
- `/mail/config-v1.1.xml`

The gateway can also serve `/mail/config-v1.1.xml` for
`autoconfig.example.org` when DNS and proxy hostnames point that subdomain at
the gateway.

If `manual_setup.url` is set, Thunderbird Autoconfig includes a documentation
link to that URL. The mail account module also serves a human-readable setup
template from the same profile data:

- `/mail/setup`

The built-in English template lives at `mail-setup.md`; deployments can
override it by mounting `mail-setup.md` in the overlay root. Translations are
stored as gettext PO files such as `translations/nb.po` and selected with
`?lang=nb`. The renderer replaces placeholders such as `{{email_address}}`,
`{{incoming.hostname}}`, and `{{outgoing.username}}` on the server. If no
`emailaddress` query parameter is provided, the page uses human-readable
descriptors such as `your full email address`.

Deployments can append site-specific help to `/mail/setup` with
`manual_setup.extra_sections`. Each section has an optional `lang`, a
plain-text `title`, and a `body_markdown` value. Untagged sections are shown for
all languages; tagged sections are shown only when `?lang=` matches. The
rendered Markdown supports paragraphs, links, and simple unordered lists.

The module also serves an unsigned Apple configuration profile:

- `/.well-known/mail/apple.mobileconfig`

Deployments can opt into share preview metadata for apps such as Telegram,
Signal, WhatsApp, and Slack with `manual_setup.share_preview.enabled`. When
enabled, the setup page includes OpenGraph and Twitter summary metadata and
serves a built-in default preview image:

- `/mail/setup-og.png`

Apple profiles should normally be requested with an email address so
placeholders such as `%EMAILADDRESS%` can be filled:

```text
/.well-known/mail/apple.mobileconfig?emailaddress=user@example.org
```

The module also serves Outlook Autodiscover XML:

- `/Autodiscover/Autodiscover.xml`
- `/AutoDiscover/AutoDiscover.xml`
- `/autodiscover/autodiscover.xml`

Autodiscover clients normally `POST` XML containing `EMailAddress`; the
renderer uses that address for profile selection and username placeholder
substitution. For local rendering and simple checks, the same route can also
use `?emailaddress=user@example.org`.

## Usage

Validate a config:

```sh
go run ./cmd/wellknown-overlay validate -config examples/overlay.json
```

Serve the configured overlay:

```sh
go run ./cmd/wellknown-overlay serve -config examples/overlay.json -root examples/public -listen 127.0.0.1:8765
```

Render a route from the command line:

```sh
go run ./cmd/wellknown-overlay render -config examples/overlay.json -root examples/public -path /.well-known/security.txt
```

Check a running overlay:

```sh
go run ./cmd/wellknown-overlay healthcheck -url http://127.0.0.1:8765/healthz
```

The HTTP server always exposes `GET` and `HEAD` on `/healthz`. The endpoint is
for process health only; configured standards routes are still matched exactly.

Run live client-autosetup checks for a deployed domain:

```sh
go run ./cmd/wellknown-overlay-livecheck user@example.org
go run ./cmd/wellknown-overlay-livecheck user@example.org THUNDERBIRD,OUTLOOK
go run ./cmd/wellknown-overlay-livecheck -insecure user@example.org APPLE
go run ./cmd/wellknown-overlay-livecheck -skip-mail-auth-dns user@example.org
go run ./cmd/wellknown-overlay-livecheck -dkim-selectors mail2026,default user@example.org
LIVECHECK_PASSWORD='...' go run ./cmd/wellknown-overlay-livecheck -round-trip user@example.org
go run ./cmd/wellknown-overlay-livecheck -livecheck-config .local/livecheck.local -round-trip user@example.org
go run ./cmd/wellknown-overlay-livecheck -livecheck-config .local/livecheck.local -round-trip
```

`wellknown-overlay-livecheck` is a diagnostic companion tool. It derives the
domain from the email address, checks the selected client discovery profiles,
prints DNS, redirect, HTTP, TLS, and response-format details, and exits non-zero
when any selected profile fails. Supported profiles are `THUNDERBIRD`,
`OUTLOOK`, `APPLE`, and `ALL`; `ALL` is the default. Redirects are allowed and
reported. TLS certificates are verified by default; `-insecure` or `-k` disables
certificate verification for diagnosis only. Suggestions are intentionally
deployment-neutral and refer to DNS, HTTPS certificates, reverse proxy/ingress
routing, and overlay endpoint reachability. The check also reports RFC 6186 and
Autodiscover SRV records, deriving suggested DNS records from the rendered mail
account settings without failing otherwise-working HTTP discovery checks.
By default it also reports advisory SPF, DMARC, and DKIM mail-auth DNS status.
SPF and DMARC are checked via TXT records. DKIM selector DNS records are checked
when selectors are supplied with `-dkim-selectors` or `DKIM_SELECTORS`.
End-to-end DKIM message signing is still reported as not tested. Use
`-skip-mail-auth-dns` to suppress these advisory mail-auth checks.

Round-trip testing is opt-in with `-round-trip`. It uses the discovered
Thunderbird Autoconfig IMAP/SMTP settings, sends a unique message to the tested
address, polls `INBOX` until that message arrives, and deletes the test message
unless `-round-trip-keep-message` is set. If `LIVECHECK_RECEIVER_EMAIL` is set,
the message is sent from the tested address to that separate receiver mailbox
instead; this is useful for checking externally delivered DKIM signatures. If no
receiver is configured, the tested address is used for both sending and
receiving. The sender password is read from `LIVECHECK_PASSWORD`,
`-livecheck-config`, or prompted interactively with hidden input. A separate
receiver mailbox requires `LIVECHECK_RECEIVER_PASSWORD`. Unlike the advisory DNS
checks, a requested round-trip failure exits non-zero.

`-livecheck-config` reads a local `KEY=value` file. It supports
`LIVECHECK_EMAIL`, `LIVECHECK_PASSWORD`, `LIVECHECK_RECEIVER_EMAIL`,
`LIVECHECK_RECEIVER_PASSWORD`, and `DKIM_SELECTORS`; environment variables
override file values, and a positional email argument overrides
`LIVECHECK_EMAIL`. Keep this file outside version control, for example under
`.local/`.

```sh
LIVECHECK_EMAIL='sender@example.org'
LIVECHECK_PASSWORD='sender password'
LIVECHECK_RECEIVER_EMAIL='receiver@example.net'
LIVECHECK_RECEIVER_PASSWORD='receiver password'
DKIM_SELECTORS='mail2026,default'
```

See `COVERAGE.md` for expected and manually verified mail-client discovery
support.

## Reverse Proxy Sketch

```nginx
location = /healthz {
    proxy_pass http://127.0.0.1:8765;
}

location ^~ /.well-known/ {
    proxy_pass http://127.0.0.1:8765;
}

location = /Autodiscover/Autodiscover.xml {
    proxy_pass http://127.0.0.1:8765;
}

location / {
    proxy_pass http://main-app;
}
```

In practice, the proxy should route only the paths the overlay owns whenever
that is convenient. The overlay itself still refuses undeclared paths.

## Docker

The root `Dockerfile` builds the minimal overlay image. It includes the
standalone `wellknown-overlay` binary plus a small Docker entrypoint helper.
For the common mail-account case, the helper can generate `overlay.json` from
environment variables at startup:

```sh
docker build -t wellknown-overlay .
docker run --rm -p 8765:8765 \
  -e MAIL_DOMAIN=example.org \
  -e MAIL_DISPLAY_NAME="Example Mail" \
  -e MAIL_INCOMING_HOST=mail.example.org \
  -e MAIL_OUTGOING_HOST=mail.example.org \
  wellknown-overlay
```

If `/etc/wellknown-overlay/overlay.json` already exists, it is treated as a
complete user-provided config and `MAIL_*` variables are ignored with a warning.
Mount `overlay.json` when you need static routes, multiple profiles, or other
advanced config:

```sh
docker run --rm -p 8765:8765 \
  -v "$PWD/examples/overlay.json:/etc/wellknown-overlay/overlay.json:ro" \
  -v "$PWD/examples/public:/var/lib/wellknown-overlay/public:ro" \
  wellknown-overlay
```

Common `MAIL_*` variables:

- `MAIL_DOMAIN` is required when generating config.
- `MAIL_DISPLAY_NAME` defaults to `MAIL_DOMAIN`.
- `MAIL_DISPLAY_SHORT_NAME` is optional.
- `MAIL_INCOMING_HOST` and `MAIL_OUTGOING_HOST` are required.
- `MAIL_INCOMING_TYPE` defaults to `imap`; `MAIL_OUTGOING_TYPE` defaults to
  `smtp`.
- `MAIL_INCOMING_PORT` defaults to `993`; `MAIL_OUTGOING_PORT` defaults to
  `587`.
- `MAIL_INCOMING_SOCKET_TYPE` defaults to `SSL`;
  `MAIL_OUTGOING_SOCKET_TYPE` defaults to `STARTTLS`.
- `MAIL_INCOMING_AUTHENTICATION` and `MAIL_OUTGOING_AUTHENTICATION` default to
  `password-cleartext`.
- `MAIL_USERNAME` defaults to `%EMAILADDRESS%` and is used for both directions
  unless `MAIL_INCOMING_USERNAME` or `MAIL_OUTGOING_USERNAME` are set.
- `MAIL_SETUP_URL` is optional. When set, generated Thunderbird Autoconfig XML
  links to that human-readable setup page.
- `MAIL_SETUP_EXTRA_SECTION_1_TITLE` and
  `MAIL_SETUP_EXTRA_SECTION_1_BODY_MARKDOWN` append a site-specific section to
  `/mail/setup`. Increase the number for additional sections, up to 20.
- `MAIL_SETUP_EXTRA_SECTION_1_LANG` optionally limits that section to a
  normalized language code such as `en` or `nb`.
- `MAIL_SETUP_SHARE_PREVIEW` enables OpenGraph and Twitter summary metadata
  when set to `1`, `true`, `yes`, or `on`.

If gateway-only variables such as `BACKEND_URL` are set on the core image, the
Docker entrypoint helper warns that they only affect the gateway image. The
standalone `wellknown-overlay` binary does not read these deployment variables.

The optional gateway image is derived from nginx. It runs the overlay locally
and proxies selected standards paths to it. If `BACKEND_URL` is set, every other
path is proxied to that backend. If `BACKEND_URL` is unset, nginx serves a small
static page explaining that the host is an automatic-configuration endpoint.

```sh
docker build -f docker/gateway/Dockerfile -t wellknown-overlay-gateway .
docker run --rm -p 8080:80 \
  -e MAIL_DOMAIN=example.org \
  -e MAIL_DISPLAY_NAME="Example Mail" \
  -e MAIL_INCOMING_HOST=mail.example.org \
  -e MAIL_OUTGOING_HOST=mail.example.org \
  -e BACKEND_URL=http://app:3000 \
  wellknown-overlay-gateway
```

For domains where the same gateway answers both the main website and
autoconfiguration subdomains, set host lists:

```sh
docker run --rm -p 8080:80 \
  -e MAIL_DOMAIN=example.org \
  -e MAIL_INCOMING_HOST=mail.example.org \
  -e MAIL_OUTGOING_HOST=mail.example.org \
  -e BACKEND_URL=http://app:3000 \
  -e BACKEND_HOSTS=example.org \
  -e OVERLAY_ONLY_HOSTS=autoconfig.example.org,autodiscover.example.org \
  wellknown-overlay-gateway
```

`BACKEND_HOSTS` use the backend fallback. `OVERLAY_ONLY_HOSTS` use the static
fallback page. If either variable is set, the lists enumerate the known hosts;
unmatched hosts return `404` except for `/healthz`. A literal `*` means the
default for hosts not otherwise matched. Putting `*` in both lists, listing the
same concrete host in both lists, or setting `BACKEND_HOSTS` without
`BACKEND_URL` is a startup configuration error.

The gateway currently routes these paths to the overlay:

- `/healthz`
- `/.well-known/autoconfig/`
- `/.well-known/openpgpkey/`
- `/.well-known/mail/apple.mobileconfig`
- `/.well-known/security.txt`
- `/.well-known/mta-sts.txt`
- `/mail/config-v1.1.xml`
- `/mail/setup`
- `/Autodiscover/Autodiscover.xml`
- `/AutoDiscover/AutoDiscover.xml`
- `/autodiscover/autodiscover.xml`

Run the gateway integration checks with Docker:

```sh
WELLKNOWN_OVERLAY_INTEGRATION=1 go test ./docker/gateway -run TestGatewayIntegration -count=1 -v
```

The check builds the gateway image, starts it from `MAIL_*` variables, and
verifies health, overlay routes, static fallback, backend proxying, and
host-aware routing.
