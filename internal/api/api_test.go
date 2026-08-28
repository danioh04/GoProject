package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"geoduel/internal/hub"
	"geoduel/internal/room"
	"geoduel/internal/store"
	"geoduel/internal/wsutil"
)

type roomSnapshot struct {
	RoomID       string `json:"room_id"`
	JoinCode     string `json:"join_code"`
	State        string `json:"state"`
	PlayerCount  int    `json:"player_count"`
	HostNickname string `json:"host_nickname"`
}

type fakeStats struct {
	stats  []store.LocationStat
	detail *store.GameDetail
}

func (f *fakeStats) HardestLocations(ctx context.Context, limit int) ([]store.LocationStat, error) {
	if limit > len(f.stats) {
		limit = len(f.stats)
	}
	return f.stats[:limit], nil
}

func (f *fakeStats) GameDetail(ctx context.Context, id string) (*store.GameDetail, error) {
	if f.detail == nil || id != f.detail.ID {
		return nil, store.ErrNotFound
	}
	return f.detail, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestHub() *hub.Hub {
	return hub.New(testLogger(), room.Options{})
}

// -----------------------------------------------------------------------------
// Middleware Tests
// -----------------------------------------------------------------------------

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

// -----------------------------------------------------------------------------
// Health, Root, and Config Endpoint Tests
// -----------------------------------------------------------------------------

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
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
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
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
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil, "test-api-key-xyz"))
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

func TestRootEndpoint(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
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
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
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

// -----------------------------------------------------------------------------
// Room Lifecycle Tests
// -----------------------------------------------------------------------------

func TestCreateAndPreviewRoom(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
	defer srv.Close()

	resp := postJSON(t, srv, "/v1/rooms", `{"nickname": "dan"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	created := decode[roomSnapshot](t, resp)
	if created.JoinCode == "" || created.RoomID == "" {
		t.Fatalf("missing identity in response: %+v", created)
	}
	if created.State != "lobby" || created.PlayerCount != 0 || created.HostNickname != "dan" {
		t.Errorf("unexpected snapshot: %+v", created)
	}

	resp2 := mustGet(t, srv, "/v1/rooms/"+created.JoinCode)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
	preview := decode[roomSnapshot](t, resp2)
	if preview != created {
		t.Errorf("preview = %+v, want %+v", preview, created)
	}

	lower := mustGet(t, srv, "/v1/rooms/"+strings.ToLower(created.JoinCode))
	defer lower.Body.Close()
	if lower.StatusCode != http.StatusOK {
		t.Errorf("lowercase preview status = %d, want %d", lower.StatusCode, http.StatusOK)
	}
}

func TestCreateRoomValidation(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
	defer srv.Close()

	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"blank nickname", `{"nickname": "   "}`, http.StatusBadRequest},
		{"missing field", `{}`, http.StatusBadRequest},
		{"nickname too long", `{"nickname": "` + strings.Repeat("x", 25) + `"}`, http.StatusBadRequest},
		{"unknown field", `{"nickname": "dan", "extra": 1}`, http.StatusBadRequest},
		{"malformed json", `{"nickname": `, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv, "/v1/rooms", tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
		})
	}
}

func TestPreviewRoomErrors(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
	defer srv.Close()

	postJSON(t, srv, "/v1/rooms", `{"nickname": "dan"}`).Body.Close()

	cases := []struct {
		name   string
		code   string
		status int
	}{
		{"well-formed but unknown", "AAAAAA", http.StatusNotFound},
		{"too short", "ABC", http.StatusBadRequest},
		{"ambiguous chars", "0O1I23", http.StatusBadRequest},
		{"lowercase invalid char", "abc12!", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := mustGet(t, srv, "/v1/rooms/"+tc.code)
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Stats Endpoint Tests
// -----------------------------------------------------------------------------

func TestStatsDisabledWithoutPersistence(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
	defer srv.Close()

	for _, path := range []string{"/v1/stats/hardest", "/v1/games/whatever"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s status = %d, want 503", path, resp.StatusCode)
		}
	}
}

func TestHardestLocationsEndpoint(t *testing.T) {
	fake := &fakeStats{
		stats: []store.LocationStat{
			{LocationID: "yakutsk", Samples: 7, AvgMissKM: 4210.5},
			{LocationID: "paris", Samples: 12, AvgMissKM: 830.2},
		},
	}
	srv := httptest.NewServer(New(testLogger(), newTestHub(), fake))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/v1/stats/hardest?limit=1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got struct {
		Locations []struct {
			LocationID string  `json:"location_id"`
			Samples    int64   `json:"samples"`
			AvgMissKM  float64 `json:"avg_miss_km"`
		} `json:"locations"`
	}
	json.Unmarshal(body, &got)
	if len(got.Locations) != 1 || got.Locations[0].LocationID != "yakutsk" {
		t.Errorf("limit not applied or wrong data: %s", body)
	}

	bad, err := srv.Client().Get(srv.URL + "/v1/stats/hardest?limit=99")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("limit=99 status = %d, want 400", bad.StatusCode)
	}
}

func TestGameDetailEndpoint(t *testing.T) {
	fake := &fakeStats{
		detail: &store.GameDetail{
			ID:          "game-1",
			TotalRounds: 5,
			Standings: []store.StandingRow{
				{PlayerID: "p1", Nickname: "alice", TotalScore: 12345, Placement: 1},
			},
		},
	}
	srv := httptest.NewServer(New(testLogger(), newTestHub(), fake))
	defer srv.Close()

	okResp, err := srv.Client().Get(srv.URL + "/v1/games/game-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(okResp.Body)
	okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"placement":1`) {
		t.Errorf("detail response wrong: %d %s", okResp.StatusCode, body)
	}

	missing, err := srv.Client().Get(srv.URL + "/v1/games/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("missing game status = %d, want 404", missing.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// WebSocket API Upgrade Tests
// -----------------------------------------------------------------------------

func TestWSRejectsBeforeUpgrade(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
	defer srv.Close()

	cases := []struct {
		name   string
		query  string
		status int
	}{
		{"missing name", "?code=ABC234", http.StatusBadRequest},
		{"bad name", "?code=ABC234&name=" + strings.Repeat("x", 30), http.StatusBadRequest},
		{"invalid code", "?code=0O1I23&name=dan", http.StatusBadRequest},
		{"unknown room", "?code=ABC234&name=dan", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + "/v1/ws" + tc.query)
			if err != nil {
				t.Fatalf("GET /v1/ws: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			ct := resp.Header.Get("Content-Type")
			if ct != "application/json" && tc.status != http.StatusNotFound {
				t.Errorf("content-type = %q, want application/json", ct)
			}
		})
	}
}

func TestWSEndToEndThroughAPI(t *testing.T) {
	srv := httptest.NewServer(New(testLogger(), newTestHub(), nil))
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/v1/rooms", "application/json", strings.NewReader(`{"nickname":"host"}`))
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var created struct {
		JoinCode string `json:"join_code"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create response %q: %v", body, err)
	}

	wsURL := "ws" + srv.URL[len("http"):] + "/v1/ws?code=" + created.JoinCode + "&name=host"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read joined: %v", err)
	}
	env, err := wsutil.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Type != "joined" {
		t.Errorf("first message type = %q, want joined", env.Type)
	}
}

// -----------------------------------------------------------------------------
// Test Helpers
// -----------------------------------------------------------------------------

func postJSON(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := srv.Client().Post(srv.URL+path, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func mustGet(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return v
}
