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

		assertGET(t, baseURL+"/", http.StatusOK, "backend ok\n")
		assertGET(t, baseURL+"/healthz", http.StatusOK, "ok\n")
		assertGET(t, baseURL+"/mail/config-v1.1.xml", http.StatusOK, "<clientConfig")
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
		assertPOSTHost(t, baseURL, "autodiscover.example.org", "/Autodiscover/Autodiscover.xml", autodiscoverRequest("alice@example.org"), http.StatusOK, "<Autodiscover")
		assertGETHost(t, baseURL, "stray.example.org", "/", http.StatusNotFound, "")
	})
}

func runGateway(t *testing.T, ctx context.Context, repoRoot, image, network, backendURL string, extraEnv ...string) string {
	t.Helper()

	name := fmt.Sprintf("wellknown-overlay-gateway-%d", time.Now().UnixNano())
	args := []string{
		"run", "-d",
		"--name", name,
		"--network", network,
		"-p", "127.0.0.1::80",
		"-v", t.TempDir() + ":/var/lib/wellknown-overlay/public:ro",
		"-e", "MAIL_DOMAIN=example.org",
		"-e", "MAIL_DISPLAY_NAME=Example Mail",
		"-e", "MAIL_INCOMING_HOST=mail.example.org",
		"-e", "MAIL_OUTGOING_HOST=mail.example.org",
	}
	if backendURL != "" {
		args = append(args, "-e", "BACKEND_URL="+backendURL)
	}
	for _, env := range extraEnv {
		args = append(args, "-e", env)
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
