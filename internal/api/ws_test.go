package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"geoduel/internal/api"
	"geoduel/internal/hub"
	"geoduel/internal/wsutil"
)

func TestWSRejectsBeforeUpgrade(t *testing.T) {
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), 8)))
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
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), 8)))
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
