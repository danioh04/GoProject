package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"geoduel/internal/api"
	"geoduel/internal/hub"
	"geoduel/internal/room"
)

func newTestHub() *hub.Hub {
	return hub.New(slog.New(slog.DiscardHandler), room.Options{})
}

func TestRootEndpoint(t *testing.T) {
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
	var meta map[string]any
	if err := json.Unmarshal(body, &meta); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if meta["service"] != "geoduel" || meta["status"] != "ok" {
		t.Errorf("unexpected root payload: %+v", meta)
	}
}

func TestUnknownPathReturns404(t *testing.T) {
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
