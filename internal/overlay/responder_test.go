package overlay

import (
	"errors"
	"net/http"
	"testing"
	"testing/fstest"
)

func TestResponderRendersConfiguredStaticRoute(t *testing.T) {
	cfg := Config{
		Routes: []Route{
			{
				Path:        "/.well-known/security.txt",
				File:        "security.txt",
				ContentType: "text/plain; charset=utf-8",
			},
		},
	}
	files := fstest.MapFS{
		"security.txt": {Data: []byte("Contact: mailto:security@example.org\n")},
	}

	responder := NewResponder(cfg, files)
	response, err := responder.Render("/.well-known/security.txt")
	if err != nil {
		t.Fatal(err)
	}

	if response.Status != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Status, http.StatusOK)
	}
	if string(response.Body) != "Contact: mailto:security@example.org\n" {
		t.Fatalf("unexpected body %q", response.Body)
	}
	if response.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", response.ContentType)
	}
}

func TestResponderRejectsUndeclaredRoute(t *testing.T) {
	responder := NewResponder(Config{
		Routes: []Route{{Path: "/declared", File: "declared.txt"}},
	}, fstest.MapFS{})

	_, err := responder.Render("/other")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
