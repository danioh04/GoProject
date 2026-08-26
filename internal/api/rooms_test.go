package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"geoduel/internal/api"
	"geoduel/internal/hub"
	"geoduel/internal/room"
)

type roomSnapshot struct {
	RoomID       string `json:"room_id"`
	JoinCode     string `json:"join_code"`
	State        string `json:"state"`
	PlayerCount  int    `json:"player_count"`
	HostNickname string `json:"host_nickname"`
}

func TestCreateAndPreviewRoom(t *testing.T) {
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil))
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
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil))
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
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil))
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
