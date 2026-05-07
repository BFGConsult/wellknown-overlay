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
    "profiles": [
      {
        "match": "*+bfg@efn.no",
        "domain": "efn.no",
        "display_name": "EFN",
        "incoming": {
          "type": "imap",
          "hostname": "login.kristshell.net",
          "port": 993,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "bfg@efn.no"
        },
        "outgoing": {
          "type": "smtp",
          "hostname": "login.kristshell.net",
          "port": 587,
          "socket_type": "STARTTLS",
          "authentication": "password-cleartext",
          "username": "bfg@efn.no"
        }
      },
      {
        "match": "default",
        "domain": "efn.no",
        "display_name": "EFN",
        "incoming": {
          "type": "imap",
          "hostname": "login.kristshell.net",
          "port": 993,
          "socket_type": "SSL",
          "authentication": "password-cleartext",
          "username": "%EMAILADDRESS%"
        },
        "outgoing": {
          "type": "smtp",
          "hostname": "login.kristshell.net",
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

Profile matching uses the request's `emailaddress` query parameter. The profile
with `match: "default"` is required and is used when no other profile matches.
All other `match` values are case-insensitive glob patterns against the full
email address; `*` matches any sequence and `?` matches one character. More
literal characters beat fewer literal characters, fewer wildcards break that
tie, and config order breaks any remaining tie.

When enabled, the module owns these exact Thunderbird routes:

- `/.well-known/autoconfig/mail/config-v1.1.xml`
- `/mail/config-v1.1.xml`

The gateway can also serve `/mail/config-v1.1.xml` for
`autoconfig.example.org` when DNS and proxy hostnames point that subdomain at
the gateway.

The module also serves an unsigned Apple configuration profile:

- `/.well-known/mail/apple.mobileconfig`

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

The root `Dockerfile` builds the minimal overlay image. It runs only the Go
binary and expects a config file plus a public file tree to be mounted in:

```sh
docker build -t wellknown-overlay .
docker run --rm -p 8765:8765 \
  -v "$PWD/examples/overlay.json:/etc/wellknown-overlay/overlay.json:ro" \
  -v "$PWD/examples/public:/var/lib/wellknown-overlay/public:ro" \
  wellknown-overlay
```

The optional gateway image is derived from nginx. It runs the overlay locally
and proxies selected standards paths to it. If `BACKEND_URL` is set, every other
path is proxied to that backend. If `BACKEND_URL` is unset, nginx serves a small
placeholder page instead.

```sh
docker build -f docker/gateway/Dockerfile -t wellknown-overlay-gateway .
docker run --rm -p 8080:80 \
  -e BACKEND_URL=http://app:3000 \
  -v "$PWD/examples/legacy-email/overlay.json:/etc/wellknown-overlay/overlay.json:ro" \
  -v "$PWD/examples/legacy-email/public:/var/lib/wellknown-overlay/public:ro" \
  wellknown-overlay-gateway
```

The gateway currently routes these paths to the overlay:

- `/healthz`
- `/.well-known/autoconfig/`
- `/.well-known/openpgpkey/`
- `/.well-known/mail/apple.mobileconfig`
- `/.well-known/security.txt`
- `/.well-known/mta-sts.txt`
- `/mail/config-v1.1.xml`
- `/Autodiscover/Autodiscover.xml`

Run the gateway integration checks with Docker:

```sh
WELLKNOWN_OVERLAY_INTEGRATION=1 go test ./docker/gateway -run TestGatewayIntegration -count=1 -v
```

The check builds the gateway image, starts it with the email autoconfig example,
and verifies health, overlay routes, placeholder fallback, and backend proxying.
