package gatewayconfig

import (
	"fmt"
	"io"
	"mime"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

// RenderTargetLocations writes exact nginx locations for routes assigned to
// one gateway host class. Untargeted routes remain handled by the overlay
// server's existing locations and responder.
func RenderTargetLocations(w io.Writer, cfg overlay.Config, root, target, backendURL string) error {
	if target != overlay.RouteTargetBackend && target != overlay.RouteTargetOverlayOnly {
		return fmt.Errorf("unsupported gateway route target %q", target)
	}

	for _, route := range cfg.Routes {
		if route.Target != target {
			continue
		}

		if route.Status != 0 {
			fmt.Fprintf(w, "    location = %s {\n", nginxQuote(route.Path))
			fmt.Fprintf(w, "        return %d;\n", route.Status)
			fmt.Fprintln(w, "    }")
			fmt.Fprintln(w)
			continue
		}

		switch route.Source {
		case "":
			fmt.Fprintf(w, "    location = %s {\n", nginxQuote(route.Path))
			contentType := route.ContentType
			if contentType == "" {
				contentType = mime.TypeByExtension(filepath.Ext(route.File))
			}
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			fmt.Fprintln(w, "        types { }")
			fmt.Fprintf(w, "        default_type %s;\n", nginxQuote(contentType))
			fmt.Fprintf(w, "        alias %s;\n", nginxQuote(filepath.Join(root, filepath.FromSlash(route.File))))
			fmt.Fprintln(w, "    }")
			fmt.Fprintln(w)
		case overlay.RouteSourceBackend:
			if backendURL == "" {
				return fmt.Errorf("route %q sources the backend but BACKEND_URL is empty", route.Path)
			}
			upstreamURL, err := backendSourceURL(backendURL, route.Path)
			if err != nil {
				return fmt.Errorf("route %q: %w", route.Path, err)
			}
			fmt.Fprintf(w, "    location = %s {\n", nginxQuote(route.Path))
			renderBackendProxy(w, route, upstreamURL)
			fmt.Fprintln(w, "    }")
			fmt.Fprintln(w)
		default:
			return fmt.Errorf("unsupported source %q for route %q", route.Source, route.Path)
		}
	}

	return nil
}

func renderBackendProxy(w io.Writer, route overlay.Route, upstreamURL string) {
	fmt.Fprintln(w, "        limit_except GET { deny all; }")
	fmt.Fprintln(w, "        proxy_cache wellknown_overlay_routes;")
	fmt.Fprintf(w, "        proxy_cache_key %s;\n", nginxQuote(route.SourceHost+"|"+route.Path))
	fmt.Fprintln(w, "        proxy_cache_lock on;")
	fmt.Fprintln(w, "        proxy_cache_background_update on;")
	fmt.Fprintln(w, "        proxy_cache_revalidate on;")
	fmt.Fprintln(w, "        proxy_cache_use_stale error timeout invalid_header updating http_500 http_502 http_503 http_504;")
	cacheStatuses := route.CacheStatuses
	if len(cacheStatuses) == 0 {
		cacheStatuses = []int{200}
	}
	statusValues := make([]string, 0, len(cacheStatuses))
	for _, status := range cacheStatuses {
		statusValues = append(statusValues, strconv.Itoa(status))
	}
	fmt.Fprintf(w, "        proxy_cache_valid %s 1h;\n", strings.Join(statusValues, " "))
	fmt.Fprintln(w, "        proxy_ignore_headers X-Accel-Expires Expires Cache-Control Set-Cookie Vary;")
	fmt.Fprintln(w, "        proxy_hide_header Set-Cookie;")
	fmt.Fprintln(w, "        proxy_method GET;")
	fmt.Fprintln(w, "        proxy_pass_request_body off;")
	fmt.Fprintln(w, "        proxy_pass_request_headers off;")
	fmt.Fprintf(w, "        proxy_set_header Host %s;\n", nginxQuote(route.SourceHost))
	fmt.Fprintf(w, "        proxy_pass %s;\n", nginxQuote(upstreamURL))
}

func backendSourceURL(backendURL, sourcePath string) (string, error) {
	parsed, err := url.Parse(backendURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("BACKEND_URL must be an absolute HTTP(S) URL")
	}
	parsed.Path = sourcePath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = true
	parsed.Fragment = ""
	return parsed.String(), nil
}

func nginxQuote(value string) string {
	return `"` + nginxEscape(value) + `"`
}

func nginxEscape(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`$`, `\$`,
		"\r", `\r`,
		"\n", `\n`,
	)
	return replacer.Replace(value)
}
