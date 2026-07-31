package gateway_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatewayIntegration(t *testing.T) {
	if os.Getenv("WELLKNOWN_OVERLAY_INTEGRATION") != "1" {
		t.Skip("set WELLKNOWN_OVERLAY_INTEGRATION=1 to run Docker gateway integration checks")
	}

	repoRoot := findRepoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	image := fmt.Sprintf("wellknown-overlay-gateway-integration:%d", time.Now().UnixNano())
	runDocker(t, ctx, repoRoot, "build", "-f", "docker/gateway/Dockerfile", "-t", image, ".")
	t.Cleanup(func() {
		cleanupDocker(t, context.Background(), "rmi", "-f", image)
	})

	network := fmt.Sprintf("wellknown-overlay-gateway-%d", time.Now().UnixNano())
	runDocker(t, ctx, repoRoot, "network", "create", network)
	t.Cleanup(func() {
		cleanupDocker(t, context.Background(), "network", "rm", network)
	})

	t.Run("without backend", func(t *testing.T) {
		gatewayName := runGateway(t, ctx, repoRoot, image, network, "")
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", gatewayName)
		})

		baseURL := gatewayBaseURL(t, ctx, repoRoot, gatewayName)
		waitForHealth(t, baseURL)

		assertGET(t, baseURL+"/healthz", http.StatusOK, "ok\n")

		modernBody := assertGET(t, baseURL+"/.well-known/autoconfig/mail/config-v1.1.xml", http.StatusOK, "")
		legacyBody := assertGET(t, baseURL+"/mail/config-v1.1.xml", http.StatusOK, "")
		if modernBody != legacyBody {
			t.Fatal("legacy email routes served different bodies")
		}
		assertGET(t, baseURL+"/.well-known/mail/apple.mobileconfig?emailaddress=alice@example.org", http.StatusOK, "com.apple.mail.managed")
		assertPOST(t, baseURL+"/Autodiscover/Autodiscover.xml", autodiscoverRequest("alice@example.org"), http.StatusOK, "<LoginName>alice@example.org</LoginName>")
		assertPOST(t, baseURL+"/autodiscover/autodiscover.xml", autodiscoverRequest("person+help@example.org"), http.StatusOK, "<LoginName>person+help@example.org</LoginName>")

		assertGET(t, baseURL+"/.well-known/openpgpkey/example", http.StatusNotFound, "")
		assertGET(t, baseURL+"/", http.StatusOK, "wellknown-overlay is running")
	})

	t.Run("with backend", func(t *testing.T) {
		backendName := fmt.Sprintf("wellknown-overlay-backend-%d", time.Now().UnixNano())
		backendRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(backendRoot, "index.html"), []byte("backend ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runDocker(t, ctx, repoRoot, "run", "-d", "--name", backendName, "--network", network, "-v", backendRoot+":/usr/share/nginx/html:ro", "nginx:1.29-alpine")
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", backendName)
		})

		gatewayName := runGateway(t, ctx, repoRoot, image, network, "http://"+backendName)
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", gatewayName)
		})

		baseURL := gatewayBaseURL(t, ctx, repoRoot, gatewayName)
		waitForHealth(t, baseURL)

		assertBackendFallbackSupportsWebSockets(t, ctx, repoRoot, gatewayName)
		assertGET(t, baseURL+"/", http.StatusOK, "backend ok\n")
		assertGET(t, baseURL+"/healthz", http.StatusOK, "ok\n")
		assertGET(t, baseURL+"/mail/config-v1.1.xml", http.StatusOK, "<clientConfig")
		assertGET(t, baseURL+"/mail/setup?emailaddress=alice@example.org&lang=nb", http.StatusOK, "<h1>E-postoppsett for Example Mail</h1>")
		assertGET(t, baseURL+"/.well-known/mail/apple.mobileconfig?emailaddress=alice@example.org", http.StatusOK, "com.apple.mail.managed")
		assertPOST(t, baseURL+"/Autodiscover/Autodiscover.xml", autodiscoverRequest("alice@example.org"), http.StatusOK, "<Autodiscover")
	})

	t.Run("host aware backend and overlay only", func(t *testing.T) {
		backendName := fmt.Sprintf("wellknown-overlay-backend-%d", time.Now().UnixNano())
		backendRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(backendRoot, "index.html"), []byte("backend ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runDocker(t, ctx, repoRoot, "run", "-d", "--name", backendName, "--network", network, "-v", backendRoot+":/usr/share/nginx/html:ro", "nginx:1.29-alpine")
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", backendName)
		})

		gatewayName := runGateway(
			t,
			ctx,
			repoRoot,
			image,
			network,
			"http://"+backendName,
			"BACKEND_HOSTS=example.org",
			"OVERLAY_ONLY_HOSTS=autoconfig.example.org,autodiscover.example.org",
		)
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", gatewayName)
		})

		baseURL := gatewayBaseURL(t, ctx, repoRoot, gatewayName)
		waitForHealth(t, baseURL)

		assertGETHost(t, baseURL, "example.org", "/", http.StatusOK, "backend ok\n")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/", http.StatusOK, "wellknown-overlay is running")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/mail/config-v1.1.xml", http.StatusOK, "<clientConfig")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/mail/setup?emailaddress=alice@example.org", http.StatusOK, "<h1>Email setup for Example Mail</h1>")
		assertPOSTHost(t, baseURL, "autodiscover.example.org", "/Autodiscover/Autodiscover.xml", autodiscoverRequest("alice@example.org"), http.StatusOK, "<Autodiscover")
		assertGETHost(t, baseURL, "stray.example.org", "/", http.StatusNotFound, "")
	})

	t.Run("targeted routes and cached backend source", func(t *testing.T) {
		backendName := fmt.Sprintf("wellknown-overlay-backend-%d", time.Now().UnixNano())
		backendRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(backendRoot, "index.html"), []byte("backend ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		backendRobots := filepath.Join(backendRoot, "robots.txt")
		if err := os.WriteFile(backendRobots, []byte("User-agent: *\nDisallow: /backend-v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backendRoot, "sitemap.xml"), []byte("<urlset>backend sitemap</urlset>\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runDocker(t, ctx, repoRoot, "run", "-d", "--name", backendName, "--network", network, "-v", backendRoot+":/usr/share/nginx/html:ro", "nginx:1.29-alpine")
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", backendName)
		})

		overlayRoot := t.TempDir()
		if err := os.WriteFile(filepath.Join(overlayRoot, "target-backend.txt"), []byte("backend target\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(overlayRoot, "target-overlay.txt"), []byte("overlay target\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(t.TempDir(), "overlay.json")
		configBody := `{
  "routes": [
    {
      "path": "/target.txt",
      "file": "target-backend.txt",
      "content_type": "text/plain; charset=utf-8",
      "target": "backend"
    },
    {
      "path": "/target.txt",
      "file": "target-overlay.txt",
      "content_type": "text/plain; charset=utf-8",
      "target": "overlay_only"
    },
    {
      "path": "/robots.txt",
      "target": "overlay_only",
      "source": "backend",
      "source_host": "example.org"
    },
    {
      "path": "/sitemap.xml",
      "target": "overlay_only",
      "status": 404
    },
    {
      "path": "/missing.txt",
      "target": "overlay_only",
      "source": "backend",
      "source_host": "example.org",
      "cache_statuses": [404]
    }
  ]
}`
		if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
			t.Fatal(err)
		}

		gatewayName := runGatewayWithConfig(
			t,
			ctx,
			repoRoot,
			image,
			network,
			"http://"+backendName,
			configPath,
			overlayRoot,
			"BACKEND_HOSTS=example.org",
			"OVERLAY_ONLY_HOSTS=autoconfig.example.org,autodiscover.example.org",
		)
		t.Cleanup(func() {
			cleanupDocker(t, context.Background(), "rm", "-f", gatewayName)
		})

		baseURL := gatewayBaseURL(t, ctx, repoRoot, gatewayName)
		waitForHealth(t, baseURL)

		assertGETHost(t, baseURL, "example.org", "/target.txt", http.StatusOK, "backend target\n")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/target.txt", http.StatusOK, "overlay target\n")
		assertGETHost(t, baseURL, "example.org", "/robots.txt", http.StatusOK, "Disallow: /backend-v1")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/robots.txt", http.StatusOK, "Disallow: /backend-v1")
		assertGETHost(t, baseURL, "example.org", "/sitemap.xml", http.StatusOK, "backend sitemap")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/sitemap.xml", http.StatusNotFound, "")
		assertGETHost(t, baseURL, "autoconfig.example.org", "/missing.txt", http.StatusNotFound, "404 Not Found")

		if err := os.WriteFile(backendRobots, []byte("User-agent: *\nDisallow: /backend-v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backendRoot, "missing.txt"), []byte("backend now has this file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertGETHost(t, baseURL, "example.org", "/robots.txt", http.StatusOK, "Disallow: /backend-v2")
		assertGETHost(t, baseURL, "autodiscover.example.org", "/robots.txt", http.StatusOK, "Disallow: /backend-v1")
		assertGETHost(t, baseURL, "example.org", "/missing.txt", http.StatusOK, "backend now has this file")
		assertGETHost(t, baseURL, "autodiscover.example.org", "/missing.txt", http.StatusNotFound, "404 Not Found")
	})
}

func runGateway(t *testing.T, ctx context.Context, repoRoot, image, network, backendURL string, extraEnv ...string) string {
	t.Helper()

	overlayRoot := t.TempDir()
	env := []string{
		"MAIL_DOMAIN=example.org",
		"MAIL_DISPLAY_NAME=Example Mail",
		"MAIL_INCOMING_HOST=mail.example.org",
		"MAIL_OUTGOING_HOST=mail.example.org",
		"MAIL_SETUP_URL=https://autoconfig.example.org/mail/setup",
	}
	env = append(env, extraEnv...)
	return runGatewayContainer(t, ctx, repoRoot, image, network, backendURL, []string{overlayRoot + ":/var/lib/wellknown-overlay/public:ro"}, env)
}

func runGatewayWithConfig(t *testing.T, ctx context.Context, repoRoot, image, network, backendURL, configPath, overlayRoot string, extraEnv ...string) string {
	t.Helper()

	volumes := []string{
		configPath + ":/etc/wellknown-overlay/overlay.json:ro",
		overlayRoot + ":/var/lib/wellknown-overlay/public:ro",
	}
	return runGatewayContainer(t, ctx, repoRoot, image, network, backendURL, volumes, extraEnv)
}

func runGatewayContainer(t *testing.T, ctx context.Context, repoRoot, image, network, backendURL string, volumes, env []string) string {
	t.Helper()

	name := fmt.Sprintf("wellknown-overlay-gateway-%d", time.Now().UnixNano())
	args := []string{
		"run", "-d",
		"--name", name,
		"--network", network,
		"-p", "127.0.0.1::80",
	}
	for _, volume := range volumes {
		args = append(args, "-v", volume)
	}
	if backendURL != "" {
		args = append(args, "-e", "BACKEND_URL="+backendURL)
	}
	for _, value := range env {
		args = append(args, "-e", value)
	}
	args = append(args, image)

	runDocker(t, ctx, repoRoot, args...)
	return name
}

func gatewayBaseURL(t *testing.T, ctx context.Context, repoRoot, gatewayName string) string {
	t.Helper()

	out := runDockerOutput(t, ctx, repoRoot, "port", gatewayName, "80/tcp")
	hostPort := strings.TrimSpace(out)
	if hostPort == "" {
		t.Fatalf("docker port returned no mapping for %s", gatewayName)
	}
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("parse docker port %q: %v", hostPort, err)
	}
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func waitForHealth(t *testing.T, baseURL string) {
	t.Helper()

	client := http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && string(body) == "ok\n" {
				return
			}
			lastErr = fmt.Errorf("status %d body %q", resp.StatusCode, body)
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("gateway did not become healthy: %v", lastErr)
}

func assertGET(t *testing.T, url string, wantStatus int, wantBodySubstring string) string {
	t.Helper()

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return doAssert(t, client, req, wantStatus, wantBodySubstring)
}

func assertGETHost(t *testing.T, baseURL, host, path string, wantStatus int, wantBodySubstring string) string {
	t.Helper()

	client := http.Client{Timeout: 5 * time.Second}
	url := baseURL + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	req.Host = host
	return doAssert(t, client, req, wantStatus, wantBodySubstring)
}

func assertPOST(t *testing.T, url, requestBody string, wantStatus int, wantBodySubstring string) string {
	t.Helper()

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "text/xml")
	return doAssert(t, client, req, wantStatus, wantBodySubstring)
}

func assertPOSTHost(t *testing.T, baseURL, host, path, requestBody string, wantStatus int, wantBodySubstring string) string {
	t.Helper()

	client := http.Client{Timeout: 5 * time.Second}
	url := baseURL + path
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "text/xml")
	return doAssert(t, client, req, wantStatus, wantBodySubstring)
}

func doAssert(t *testing.T, client http.Client, req *http.Request, wantStatus int, wantBodySubstring string) string {
	t.Helper()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", req.URL, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s host %q status = %d, want %d; body %q", req.Method, req.URL, req.Host, resp.StatusCode, wantStatus, body)
	}
	if wantBodySubstring != "" && !strings.Contains(string(body), wantBodySubstring) {
		t.Fatalf("%s %s host %q body %q does not contain %q", req.Method, req.URL, req.Host, body, wantBodySubstring)
	}
	return string(body)
}

func autodiscoverRequest(emailAddress string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/requestschema/2006">
  <Request>
    <EMailAddress>` + emailAddress + `</EMailAddress>
    <AcceptableResponseSchema>http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a</AcceptableResponseSchema>
  </Request>
</Autodiscover>`
}

func runDocker(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	_ = runDockerOutput(t, ctx, dir, args...)
}

func runDockerOutput(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func cleanupDocker(t *testing.T, ctx context.Context, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(ctx, "docker", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("cleanup docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func assertBackendFallbackSupportsWebSockets(t *testing.T, ctx context.Context, repoRoot, gatewayName string) {
	t.Helper()

	config := runDockerOutput(t, ctx, repoRoot, "exec", gatewayName, "cat", "/etc/nginx/conf.d/default.conf")
	for _, directive := range []string{
		"map $http_upgrade $connection_upgrade",
		"proxy_http_version 1.1;",
		"proxy_set_header Upgrade $http_upgrade;",
		"proxy_set_header Connection $connection_upgrade;",
		"proxy_set_header Host $host;",
		"proxy_set_header X-Forwarded-Host $host;",
		"proxy_set_header X-Forwarded-Proto $scheme;",
		"proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
	} {
		if !strings.Contains(config, directive) {
			t.Fatalf("nginx config does not contain %q:\n%s", directive, config)
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
