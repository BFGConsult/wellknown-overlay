package overlay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHTTPHandlerServesHealthCheck(t *testing.T) {
	handler := NewHTTPHandler(NewResponder(Config{
		Routes: []Route{{Path: "/declared", File: "declared.txt"}},
	}, fstest.MapFS{}))

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + HealthPath)
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q, want text/plain; charset=utf-8", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "ok\n" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func TestHTTPHandlerAcceptsAutodiscoverPost(t *testing.T) {
	handler := NewHTTPHandler(NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{}))

	body := []byte(`<Autodiscover>
  <Request>
    <EMailAddress>bfg@example.org</EMailAddress>
  </Request>
</Autodiscover>`)
	req := httptest.NewRequest(http.MethodPost, AutodiscoverPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/xml" {
		t.Fatalf("content type = %q, want application/xml", got)
	}
	if !strings.Contains(rec.Body.String(), "<LoginName>bfg@example.org</LoginName>") {
		t.Fatalf("body does not contain substituted login:\n%s", rec.Body.String())
	}
}

func TestHTTPHandlerAddsAbsoluteSharePreviewImageURL(t *testing.T) {
	handler := NewHTTPHandler(NewResponder(Config{
		MailAccount: testMailAccountWithSharePreview(),
	}, fstest.MapFS{}))

	req := httptest.NewRequest(http.MethodGet, MailSetupPath+"?lang=nb", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "autoconfig.example.org")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	want := `<meta property="og:image" content="https://autoconfig.example.org/mail/setup-og.png?lang=nb">`
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body does not contain %q:\n%s", want, rec.Body.String())
	}
}

func TestHTTPHandlerDoesNotServeMailSetupSharePreviewImageByDefault(t *testing.T) {
	handler := NewHTTPHandler(NewResponder(Config{
		MailAccount: testMailAccount(),
	}, fstest.MapFS{}))

	req := httptest.NewRequest(http.MethodGet, MailSetupImagePath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHTTPHandlerServesMailSetupSharePreviewImageWhenEnabled(t *testing.T) {
	handler := NewHTTPHandler(NewResponder(Config{
		MailAccount: testMailAccountWithSharePreview(),
	}, fstest.MapFS{}))

	req := httptest.NewRequest(http.MethodGet, MailSetupImagePath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("body does not look like PNG: %x", rec.Body.Bytes()[:8])
	}
}

func TestHTTPHandlerServesHeadHealthCheckWithoutBody(t *testing.T) {
	handler := NewHTTPHandler(NewResponder(Config{
		Routes: []Route{{Path: "/declared", File: "declared.txt"}},
	}, fstest.MapFS{}))

	req := httptest.NewRequest(http.MethodHead, HealthPath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body length = %d, want 0", rec.Body.Len())
	}
}
