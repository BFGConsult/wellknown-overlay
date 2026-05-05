package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheckCommandAcceptsOKEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if err := healthcheck([]string{"-url", server.URL}); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
}

func TestHealthcheckCommandRejectsUnhealthyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	if err := healthcheck([]string{"-url", server.URL}); err == nil {
		t.Fatal("expected healthcheck to reject non-200 endpoint")
	}
}
