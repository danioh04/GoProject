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

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	if got["status"] != "ok" {
		t.Errorf("status field = %q, want ok", got["status"])
	}
}

func TestHealthzRejectsNonGet(t *testing.T) {
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/healthz", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestConfigEndpoint(t *testing.T) {
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil, "test-api-key-xyz"))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/v1/config")
	if err != nil {
		t.Fatalf("GET /v1/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var got struct {
		GoogleMapsAPIKey string `json:"google_maps_api_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.GoogleMapsAPIKey != "test-api-key-xyz" {
		t.Errorf("google_maps_api_key = %q, want test-api-key-xyz", got.GoogleMapsAPIKey)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
