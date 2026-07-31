package gatewayconfig

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BFGConsult/wellknown-overlay/internal/overlay"
)

func TestRenderTargetLocationsSelectsTargetAndSource(t *testing.T) {
	cfg := overlay.Config{Routes: []overlay.Route{
		{
			Path:        "/robots.txt",
			File:        "robots-backend.txt",
			ContentType: "text/plain; charset=utf-8",
			Target:      overlay.RouteTargetBackend,
		},
		{
			Path:          "/robots.txt",
			Target:        overlay.RouteTargetOverlayOnly,
			Source:        overlay.RouteSourceBackend,
			SourceHost:    "example.org",
			CacheStatuses: []int{200, 404},
		},
		{
			Path:   "/sitemap.xml",
			Target: overlay.RouteTargetOverlayOnly,
			Status: 404,
		},
	}}

	var backend bytes.Buffer
	if err := RenderTargetLocations(&backend, cfg, "/srv/overlay", overlay.RouteTargetBackend, "http://app:3000"); err != nil {
		t.Fatal(err)
	}
	backendConfig := backend.String()
	for _, want := range []string{
		`location = "/robots.txt"`,
		`default_type "text/plain; charset=utf-8"`,
		`alias "/srv/overlay/robots-backend.txt"`,
	} {
		if !strings.Contains(backendConfig, want) {
			t.Fatalf("backend config does not contain %q:\n%s", want, backendConfig)
		}
	}
	if strings.Contains(backendConfig, "proxy_cache") {
		t.Fatalf("backend config unexpectedly contains overlay-only proxy:\n%s", backendConfig)
	}

	var overlayOnly bytes.Buffer
	if err := RenderTargetLocations(&overlayOnly, cfg, "/srv/overlay", overlay.RouteTargetOverlayOnly, "http://app:3000"); err != nil {
		t.Fatal(err)
	}
	overlayConfig := overlayOnly.String()
	for _, want := range []string{
		`location = "/robots.txt"`,
		`proxy_cache wellknown_overlay_routes`,
		`proxy_cache_key "example.org|/robots.txt"`,
		`proxy_cache_valid 200 404 1h`,
		`proxy_set_header Host "example.org"`,
		`proxy_pass "http://app:3000/robots.txt?"`,
		`location = "/sitemap.xml"`,
		`return 404`,
	} {
		if !strings.Contains(overlayConfig, want) {
			t.Fatalf("overlay-only config does not contain %q:\n%s", want, overlayConfig)
		}
	}
}

func TestRenderTargetLocationsRequiresBackendURLForBackendSource(t *testing.T) {
	cfg := overlay.Config{Routes: []overlay.Route{{
		Path:       "/robots.txt",
		Target:     overlay.RouteTargetOverlayOnly,
		Source:     overlay.RouteSourceBackend,
		SourceHost: "example.org",
	}}}

	var out bytes.Buffer
	err := RenderTargetLocations(&out, cfg, "/srv/overlay", overlay.RouteTargetOverlayOnly, "")
	if err == nil || !strings.Contains(err.Error(), "BACKEND_URL is empty") {
		t.Fatalf("err = %v, want missing BACKEND_URL error", err)
	}
}
