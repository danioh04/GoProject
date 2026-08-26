package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverPanicsReturns500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := recoverPanics(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestRequestIDsMiddlewareSetsHeaderAndContext(t *testing.T) {
	var capturedID string
	handler := requestIDs(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = requestIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("X-Request-ID header missing from response")
	}
	if len(headerID) != 16 {
		t.Errorf("X-Request-ID length = %d, want 16 hex chars", len(headerID))
	}
	if capturedID != headerID {
		t.Errorf("context ID = %q, want matching header ID %q", capturedID, headerID)
	}
}

func TestLogRequestsPreservesStatusCode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := logRequests(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
