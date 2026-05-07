package overlay

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExamplesServeDeclaredRoutes(t *testing.T) {
	examples := []struct {
		name       string
		configPath string
		rootPath   string
	}{
		{
			name:       "default",
			configPath: "examples/overlay.json",
			rootPath:   "examples/public",
		},
		{
			name:       "legacy-email",
			configPath: "examples/legacy-email/overlay.json",
			rootPath:   "examples/legacy-email/public",
		},
	}

	for _, example := range examples {
		t.Run(example.name, func(t *testing.T) {
			repoRoot := findRepoRoot(t)
			configPath := filepath.Join(repoRoot, example.configPath)
			rootPath := filepath.Join(repoRoot, example.rootPath)

			cfg, err := LoadConfig(configPath)
			if err != nil {
				t.Fatalf("load config: %v", err)
			}

			fileRoot := os.DirFS(rootPath)
			server := httptest.NewServer(NewHTTPHandler(NewResponder(cfg, fileRoot)))
			t.Cleanup(server.Close)

			for _, route := range cfg.Routes {
				t.Run(route.Path, func(t *testing.T) {
					expected, err := fs.ReadFile(fileRoot, route.File)
					if err != nil {
						t.Fatalf("read fixture %q: %v", route.File, err)
					}

					resp, err := http.Get(server.URL + route.Path)
					if err != nil {
						t.Fatalf("get route: %v", err)
					}
					t.Cleanup(func() {
						_ = resp.Body.Close()
					})

					if resp.StatusCode != http.StatusOK {
						t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
					}
					if got := resp.Header.Get("Content-Type"); got != route.ContentType {
						t.Fatalf("content type = %q, want %q", got, route.ContentType)
					}

					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatalf("read response body: %v", err)
					}
					if string(body) != string(expected) {
						t.Fatalf("body mismatch for %s", route.Path)
					}
				})
			}

			resp, err := http.Get(server.URL + "/not-owned-by-overlay")
			if err != nil {
				t.Fatalf("get undeclared route: %v", err)
			}
			t.Cleanup(func() {
				_ = resp.Body.Close()
			})

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("undeclared route status = %d, want %d", resp.StatusCode, http.StatusNotFound)
			}
		})
	}
}

func TestMailAccountExampleServesModuleRoutes(t *testing.T) {
	repoRoot := findRepoRoot(t)
	configPath := filepath.Join(repoRoot, "examples/mail-account/overlay.json")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	server := httptest.NewServer(NewHTTPHandler(NewResponder(cfg, os.DirFS(repoRoot))))
	t.Cleanup(server.Close)

	var firstBody string
	for i, routePath := range ThunderbirdAutoconfigPaths() {
		resp, err := http.Get(server.URL + routePath)
		if err != nil {
			t.Fatalf("get route: %v", err)
		}
		t.Cleanup(func() {
			_ = resp.Body.Close()
		})

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/xml" {
			t.Fatalf("content type = %q, want application/xml", got)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response body: %v", err)
		}
		if i == 0 {
			firstBody = string(body)
			continue
		}
		if string(body) != firstBody {
			t.Fatalf("body mismatch for %s", routePath)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("could not find repo root")
		}
		wd = parent
	}
}
