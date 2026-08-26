package room_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"geoduel/internal/game"
	"geoduel/internal/room"
	"geoduel/internal/wsutil"
)

type testClient struct {
	conn *websocket.Conn
}

func (c *testClient) readEnvelope(t *testing.T) wsutil.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	env, err := wsutil.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode envelope %q: %v", data, err)
	}
	return env
}

func (c *testClient) send(t *testing.T, env wsutil.Envelope) {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func (c *testClient) close(t *testing.T) {
	t.Helper()
	c.conn.Close(websocket.StatusNormalClosure, "")
}

func startRoomServer(t *testing.T, r *room.Room) func(name string) *testClient {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		nickname := req.URL.Query().Get("name")
		conn, err := wsutil.Accept(w, req)
		if err != nil {
			return
		}
		session := wsutil.NewSession(conn, wsutil.SessionConfig{})
		outcome := r.Attach(session, nickname)
		if !outcome.Accepted {
			wsutil.WriteError(req.Context(), conn, outcome.Reason)
			return
		}
		playerID := outcome.PlayerID
		session.Run(func(env wsutil.Envelope) {
			r.NotifyInbound(playerID, env)
		}, func() {
			r.NotifyDisconnect(playerID)
		})
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return func(name string) *testClient {
		conn, _, err := websocket.Dial(context.Background(), wsURL+"/ws?name="+url.QueryEscape(name), nil)
		if err != nil {
			t.Fatalf("dial as %q: %v", name, err)
		}
		return &testClient{conn: conn}
	}
}

func TestAttachRosterAndIdentity(t *testing.T) {
	r := room.Start(room.Options{ID: "rm1", JoinCode: "ROOMRM", Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("alice")
	joinedA := a.readEnvelope(t)
	if joinedA.Type != wsutil.TypeJoined {
		t.Fatalf("first message = %q, want joined", joinedA.Type)
	}
	var identity struct {
		PlayerID string `json:"player_id"`
		Token    string `json:"token"`
		Host     bool   `json:"host"`
	}
	if err := json.Unmarshal(joinedA.Payload, &identity); err != nil {
		t.Fatalf("decode joined payload: %v", err)
	}
	if identity.PlayerID == "" || identity.Token == "" || !identity.Host {
		t.Errorf("bad identity payload: %+v", identity)
	}
	if roster := a.readEnvelope(t); roster.Type != wsutil.TypeRoster {
		t.Fatalf("second message = %q, want roster", roster.Type)
	}

	b := dial("bob")
	if j := b.readEnvelope(t); j.Type != wsutil.TypeJoined {
		t.Fatalf("bob first message = %q, want joined", j.Type)
	}
	rosterB := b.readEnvelope(t)
	if rosterB.Type != wsutil.TypeRoster {
		t.Fatalf("bob second message = %q, want roster", rosterB.Type)
	}
	var players struct {
		Players []game.PlayerView `json:"players"`
	}
	if err := json.Unmarshal(rosterB.Payload, &players); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	if len(players.Players) != 2 {
		t.Fatalf("roster size = %d, want 2", len(players.Players))
	}
	if players.Players[0].Nickname != "alice" || !players.Players[0].IsHost {
		t.Errorf("expected alice as host, got %+v", players.Players[0])
	}

	rosterA2 := a.readEnvelope(t)
	if err := json.Unmarshal(rosterA2.Payload, &players); err != nil {
		t.Fatalf("decode roster A: %v", err)
	}
	if len(players.Players) != 2 || string(players.Players[0].PlayerID) != identity.PlayerID {
		t.Errorf("alice's roster view wrong: %+v", players.Players)
	}

	if snap := r.Snapshot(); snap.PlayerCount != 2 {
		t.Errorf("snapshot player count = %d, want 2", snap.PlayerCount)
	}
	a.close(t)
	b.close(t)
}

func TestDuplicateNicknameRejected(t *testing.T) {
	r := room.Start(room.Options{ID: "rm2", JoinCode: "ROOMRR", Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("dan")
	a.readEnvelope(t)
	a.readEnvelope(t)

	b := dial("DAN")
	rej := b.readEnvelope(t)
	if rej.Type != wsutil.TypeError {
		t.Fatalf("rejection type = %q, want error", rej.Type)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rej.Payload, &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if !strings.Contains(payload.Error, "nickname") {
		t.Errorf("unexpected rejection reason: %q", payload.Error)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := b.conn.Read(ctx); err == nil {
		t.Error("rejected connection still open")
	} else {
		b.conn.CloseNow()
	}
}

func TestRoomFullRejected(t *testing.T) {
	r := room.Start(room.Options{ID: "rm3", JoinCode: "ROOMRF", MaxPlayers: 1, Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("alice")
	a.readEnvelope(t)
	a.readEnvelope(t)

	b := dial("bob")
	rej := b.readEnvelope(t)
	if rej.Type != wsutil.TypeError {
		t.Fatalf("rejection type = %q, want error", rej.Type)
	}
	b.conn.CloseNow()
}

func TestHostPromotionOnHostLeave(t *testing.T) {
	r := room.Start(room.Options{ID: "rm4", JoinCode: "ROOMRP", Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("alice")
	joinedA := a.readEnvelope(t)
	a.readEnvelope(t)
	b := dial("bob")
	b.readEnvelope(t)
	b.readEnvelope(t)
	a.readEnvelope(t)

	var ida struct {
		PlayerID string `json:"player_id"`
	}
	json.Unmarshal(joinedA.Payload, &ida)

	a.conn.CloseNow()

	roster := b.readEnvelope(t)
	if roster.Type != wsutil.TypeRoster {
		t.Fatalf("post-leave message = %q, want roster", roster.Type)
	}
	var players struct {
		Players []game.PlayerView `json:"players"`
	}
	if err := json.Unmarshal(roster.Payload, &players); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	if len(players.Players) != 1 {
		t.Fatalf("roster size = %d, want 1", len(players.Players))
	}
	if players.Players[0].Nickname != "bob" || !players.Players[0].IsHost {
		t.Errorf("promotion failed: %+v", players.Players[0])
	}
	if string(players.Players[0].PlayerID) == ida.PlayerID {
		t.Error("stale host id in roster")
	}

	b.close(t)
	select {
	case <-r.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("empty room did not self-destruct")
	}
}

func TestPingPongWithTokenAuth(t *testing.T) {
	r := room.Start(room.Options{ID: "rm5", JoinCode: "ROOMRT", Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("alice")
	joinedA := a.readEnvelope(t)
	a.readEnvelope(t)
	var ident struct {
		Token string `json:"token"`
	}
	json.Unmarshal(joinedA.Payload, &ident)

	a.send(t, wsutil.Envelope{Version: 1, Type: wsutil.TypePing})
	if env := a.readEnvelope(t); env.Type != wsutil.TypeError {
		t.Errorf("ping without token -> %q, want error", env.Type)
	}

	a.send(t, wsutil.Envelope{Version: 1, Type: wsutil.TypePing, Token: ident.Token + "x"})
	if env := a.readEnvelope(t); env.Type != wsutil.TypeError {
		t.Errorf("ping with bad token -> %q, want error", env.Type)
	}

	a.send(t, wsutil.Envelope{Version: 1, Type: wsutil.TypePing, Token: ident.Token})
	if env := a.readEnvelope(t); env.Type != wsutil.TypePong {
		t.Errorf("ping with good token -> %q, want pong", env.Type)
	}

	a.send(t, wsutil.Envelope{Version: 1, Type: "bogus", Token: ident.Token})
	if env := a.readEnvelope(t); env.Type != wsutil.TypeError {
		t.Errorf("bogus type -> %q, want error", env.Type)
	}

	a.send(t, wsutil.Envelope{Version: 99, Type: wsutil.TypePing, Token: ident.Token})
	if env := a.readEnvelope(t); env.Type != wsutil.TypeError {
		t.Errorf("bad version -> %q, want error", env.Type)
	}

	a.close(t)
}

func TestMalformedJSONDoesNotKillSession(t *testing.T) {
	r := room.Start(room.Options{ID: "rm6", JoinCode: "ROOMRJ", Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("alice")
	a.readEnvelope(t)
	a.readEnvelope(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.conn.Write(ctx, websocket.MessageText, []byte("{not json")); err != nil {
		t.Fatalf("raw write: %v", err)
	}
	if env := a.readEnvelope(t); env.Type != wsutil.TypeError {
		t.Fatalf("malformed reply = %q, want error", env.Type)
	}
	a.close(t)
}
