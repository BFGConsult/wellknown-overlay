# wellknown-overlay

A tiny standards-path overlay for existing domains.

`wellknown-overlay` is intended for domains where another application owns the
site, but you still need to serve small standards-owned paths such as
`/.well-known/autoconfig/...`, `/Autodiscover/Autodiscover.xml`, or future
modules such as WKD for OpenPGP.

The project starts from the idea behind `email-autoconfig-php`: keep one source
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

The initial implementation supports static route overlays. The intended next
step is to add first-class modules:

- email autoconfig
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

## Reverse Proxy Sketch

```nginx
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

