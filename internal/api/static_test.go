package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"geoduel/internal/api"
	"geoduel/internal/hub"
	"geoduel/internal/room"
)

func newTestHub() *hub.Hub {
	return hub.New(slog.New(slog.DiscardHandler), room.Options{})
}

func TestStaticIndexServed(t *testing.T) {
	srv := httptest.NewServer(api.New(newTestLoggerForStatic(), newTestHub(), nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<!doctype html>") || !strings.Contains(string(body), "GeoDuel") {
		t.Error("index.html content missing or wrong")
	}
}

func TestStaticAssetsServed(t *testing.T) {
	srv := httptest.NewServer(api.New(newTestLoggerForStatic(), newTestHub(), nil))
	defer srv.Close()

	for _, path := range []string{"/app.js", "/style.css", "/vendor/leaflet.js", "/vendor/leaflet.css"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(body) == 0 {
			t.Errorf("%s served empty body", path)
		}
	}
}

func TestUnknownPathFallsThroughToStatic(t *testing.T) {
	srv := httptest.NewServer(api.New(newTestLoggerForStatic(), newTestHub(), nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/definitely-not-here")
	if err != nil {
		t.Fatalf("GET unknown path: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func newTestLoggerForStatic() *slog.Logger { return testLogger() }
