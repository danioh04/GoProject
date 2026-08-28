package room_test

import (
	"context"
	"encoding/json"
	"geoduel/internal/game"
	"geoduel/internal/room"
	"geoduel/internal/wsutil"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// -----------------------------------------------------------------------------
// Test Helpers & Mock Fixtures
// -----------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func matchLocations() []game.Location {
	return []game.Location{
		{ID: "paris", PanoID: "0oU9oWv065S_VzV-H1G4kg", LatLng: game.LatLng{Lat: 48.8566, Lng: 2.3522}},
		{ID: "tokyo", PanoID: "bYjJc805_n1a2gC4K4j3pQ", LatLng: game.LatLng{Lat: 35.6762, Lng: 139.6503}},
	}
}

func testPicker(n int) []game.Location {
	locs := matchLocations()
	if n > len(locs) {
		n = len(locs)
	}
	return locs[:n]
}

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

func guessEnvelope(lat, lng float64) wsutil.Envelope {
	env, _ := wsutil.NewEnvelope(wsutil.TypeGuess, map[string]float64{"lat": lat, "lng": lng})
	return env
}

func assertType(t *testing.T, c *testClient, want string) wsutil.Envelope {
	t.Helper()
	env := c.readEnvelope(t)
	if env.Type != want {
		t.Fatalf("expected %s, got %s (%s)", want, env.Type, string(env.Payload))
	}
	return env
}

func assertRoundStartSafe(t *testing.T, env wsutil.Envelope) {
	t.Helper()
	if strings.Contains(string(env.Payload), "48.8566") || strings.Contains(string(env.Payload), "paris") {
		t.Fatalf("round_start leaks target location: %s", string(env.Payload))
	}
}

func verifyFirstReveal(t *testing.T, env wsutil.Envelope) {
	t.Helper()
	var payload struct {
		Round   int           `json:"round"`
		Results []roundResult `json:"results"`
	}
	json.Unmarshal(env.Payload, &payload)
	if payload.Round != 1 || len(payload.Results) != 2 {
		t.Fatalf("first reveal wrong: %s", string(env.Payload))
	}
	if payload.Results[0].Nickname != "bob" || payload.Results[0].Score != 5000 {
		t.Errorf("bob should have won round 1 with 5000: %+v", payload.Results[0])
	}
}

func verifyEveryoneMissed(t *testing.T, env wsutil.Envelope) {
	t.Helper()
	var payload struct {
		Round   int           `json:"round"`
		Results []roundResult `json:"results"`
	}
	json.Unmarshal(env.Payload, &payload)
	if payload.Round != 2 || len(payload.Results) != 2 {
		t.Fatalf("second reveal wrong: %s", string(env.Payload))
	}
	for _, r := range payload.Results {
		if r.Score != 0 {
			t.Errorf("expected 0 for missing guess, got %d for %s", r.Score, r.Nickname)
		}
	}
}

func waitForRoundStart(t *testing.T, c *testClient, wantRound int) {
	t.Helper()
	for i := 0; i < 10; i++ {
		env := c.readEnvelope(t)
		if env.Type == wsutil.TypeRoundStart {
			var p struct {
				Round int `json:"round"`
			}
			json.Unmarshal(env.Payload, &p)
			if p.Round == wantRound {
				return
			}
		}
	}
	t.Fatalf("never reached round %d", wantRound)
}

func drainUntil(t *testing.T, c *testClient, wantType string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		env := c.readEnvelope(t)
		if env.Type == wantType {
			return
		}
	}
	t.Fatalf("drained without seeing %s", wantType)
}

func waitForEnvelope(t *testing.T, c *testClient, accept func(string) bool) wsutil.Envelope {
	t.Helper()
	for i := 0; i < 12; i++ {
		env := c.readEnvelope(t)
		if accept(env.Type) {
			return env
		}
	}
	t.Fatal("expected envelope never arrived")
	return wsutil.Envelope{}
}

type roundResult struct {
	Nickname string  `json:"nickname"`
	Score    int     `json:"score"`
	Distance float64 `json:"distance_m"`
}

type recordingStore struct {
	mu    sync.Mutex
	saved []room.FinishedGame
}

func (s *recordingStore) SaveGame(ctx context.Context, g room.FinishedGame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, g)
	return nil
}

func (s *recordingStore) waitSaved(t *testing.T, want int) []room.FinishedGame {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.saved)
		s.mu.Unlock()
		if n >= want {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.saved
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("store never received %d saves", want)
	return nil
}

// -----------------------------------------------------------------------------
// Core Room Unit Tests
// -----------------------------------------------------------------------------

func TestLifecycle(t *testing.T) {
	r := room.Start(room.Options{ID: "id", JoinCode: "XYZ789", Label: "host", Logger: testLogger()})
	if snap := r.Snapshot(); snap.ID != "id" || snap.JoinCode != "XYZ789" {
		t.Fatalf("bad identity in snapshot: %s / %s", snap.ID, snap.JoinCode)
	}
	if snap := r.Snapshot(); snap.State != room.PhaseLobby || snap.HostNickname != "host" {
		t.Fatalf("bad initial snapshot: %+v", snap)
	}

	r.Close()
	select {
	case <-r.Done():
	case <-time.After(time.Second):
		t.Fatal("room did not close done channel")
	}

	if got := r.Snapshot().State; got != room.PhaseClosed {
		t.Errorf("state after close = %q, want %q", got, room.PhaseClosed)
	}

	r.Close()
}

func TestSnapshotIsCopySafe(t *testing.T) {
	r := room.Start(room.Options{ID: "id", JoinCode: "XYZ789", Label: "host", Logger: testLogger()})
	defer r.Close()

	snap := r.Snapshot()
	snap.State = "tampered"
	if snap.State != "tampered" {
		t.Fatal("expected local copy to be mutated")
	}
	if r.Snapshot().State == "tampered" {
		t.Error("Snapshot returned shared mutable state")
	}
}

func TestNotifyAfterCloseIsSafe(t *testing.T) {
	r := room.Start(room.Options{ID: "id", JoinCode: "XYZ789", Label: "host", Logger: testLogger()})
	r.Close()
	<-r.Done()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.NotifyInbound("player", wsutil.Envelope{Type: "guess"})
		r.NotifyDisconnect("player")
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify blocked on closed room")
	}
}

// -----------------------------------------------------------------------------
// WebSocket Protocol & Roster Tests
// -----------------------------------------------------------------------------

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
		Host     bool   `json:"host"`
	}
	if err := json.Unmarshal(joinedA.Payload, &identity); err != nil {
		t.Fatalf("decode joined payload: %v", err)
	}
	if identity.PlayerID == "" || !identity.Host {
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
		Players []game.Player `json:"players"`
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
		Players []game.Player `json:"players"`
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
	if snap := r.Snapshot(); snap.HostNickname != "bob" {
		t.Errorf("snapshot host nickname = %q, want bob", snap.HostNickname)
	}

	b.close(t)
	select {
	case <-r.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("empty room did not self-destruct")
	}
}

func TestUnsupportedMessageType(t *testing.T) {
	r := room.Start(room.Options{ID: "rm5", JoinCode: "ROOMRT", Logger: testLogger(), Picker: testPicker})
	dial := startRoomServer(t, r)
	defer r.Close()

	a := dial("alice")
	a.readEnvelope(t)
	a.readEnvelope(t)

	a.send(t, wsutil.Envelope{Version: 1, Type: "bogus"})
	if env := a.readEnvelope(t); env.Type != wsutil.TypeError {
		t.Errorf("bogus type -> %q, want error", env.Type)
	}

	a.send(t, wsutil.Envelope{Version: 99, Type: wsutil.TypeGuess})
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

// -----------------------------------------------------------------------------
// End-to-End Match & Persistence Tests
// -----------------------------------------------------------------------------

func TestFullMatchOverWebSockets(t *testing.T) {
	r := room.Start(room.Options{
		ID:       "match-room",
		JoinCode: "MATCHM",
		Logger:   testLogger(),
		Picker:   testPicker,
		Config: game.Config{
			Rounds:     2,
			RoundTime:  250 * time.Millisecond,
			RevealTime: 80 * time.Millisecond,
			MinPlayers: 2,
			MaxScore:   5000,
		},
	})
	dial := startRoomServer(t, r)

	a := dial("alice")
	a.readEnvelope(t)
	a.readEnvelope(t)

	b := dial("bob")
	b.readEnvelope(t)
	b.readEnvelope(t)
	a.readEnvelope(t)

	a.send(t, wsutil.Envelope{Version: 1, Type: wsutil.TypeStartGame})
	assertType(t, a, wsutil.TypeGameStart)
	rs1 := assertType(t, a, wsutil.TypeRoundStart)
	assertRoundStartSafe(t, rs1)
	assertType(t, b, wsutil.TypeGameStart)
	assertType(t, b, wsutil.TypeRoundStart)

	b.send(t, guessEnvelope(48.8566, 2.3522))
	assertType(t, b, wsutil.TypeGuessAck)

	a.send(t, guessEnvelope(0, 0))
	assertType(t, a, wsutil.TypeGuessAck)

	verifyFirstReveal(t, assertType(t, a, wsutil.TypeRoundResult))
	assertType(t, b, wsutil.TypeRoundResult)

	waitForRoundStart(t, a, 2)
	assertType(t, b, wsutil.TypeRoundStart)

	doneB := make(chan struct{})
	go func() { drainUntil(t, b, wsutil.TypeGameOver); close(doneB) }()

	rr2 := assertType(t, a, wsutil.TypeRoundResult)
	verifyEveryoneMissed(t, rr2)
	end := assertType(t, a, wsutil.TypeGameOver)
	select {
	case <-doneB:
	case <-time.After(3 * time.Second):
		t.Fatal("bob never saw game over")
	}

	var standings struct {
		Standings []game.Standing `json:"standings"`
	}
	json.Unmarshal(end.Payload, &standings)
	if len(standings.Standings) != 2 {
		t.Fatalf("standings size = %d", len(standings.Standings))
	}
	if first := standings.Standings[0]; first.Nickname != "bob" || first.Total != 5000 {
		t.Errorf("winner wrong: %+v", first)
	}

	a.close(t)
	b.close(t)
	select {
	case <-r.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("room did not clean up after final disconnects")
	}
}

func TestMatchIsPersistedOnGameOver(t *testing.T) {
	rs := &recordingStore{}
	r := room.Start(room.Options{
		ID:       "persist-room",
		JoinCode: "PERSST",
		Logger:   testLogger(),
		Picker:   testPicker,
		Config: game.Config{
			Rounds:     2,
			RoundTime:  250 * time.Millisecond,
			RevealTime: 60 * time.Millisecond,
			MinPlayers: 2,
			MaxScore:   5000,
		},
		Store: rs,
	})
	dial := startRoomServer(t, r)

	a := dial("alice")
	a.readEnvelope(t)
	a.readEnvelope(t)
	b := dial("bob")
	b.readEnvelope(t)
	b.readEnvelope(t)
	a.readEnvelope(t)

	a.send(t, wsutil.Envelope{Version: 1, Type: wsutil.TypeStartGame})
	assertType(t, a, wsutil.TypeGameStart)
	assertType(t, a, wsutil.TypeRoundStart)
	assertType(t, b, wsutil.TypeGameStart)
	assertType(t, b, wsutil.TypeRoundStart)

	b.send(t, guessEnvelope(48.8566, 2.3522))
	assertType(t, b, wsutil.TypeGuessAck)
	a.send(t, guessEnvelope(0, 0))
	assertType(t, a, wsutil.TypeGuessAck)

	assertType(t, a, wsutil.TypeRoundResult)
	assertType(t, b, wsutil.TypeRoundResult)

	for _, c := range []*testClient{a, b} {
		env := waitForEnvelope(t, c, func(typ string) bool {
			return typ == wsutil.TypeRoundStart || typ == wsutil.TypeGameOver
		})
		if env.Type == wsutil.TypeRoundStart {
			c.send(t, guessEnvelope(-33.8688, 151.2093))
			assertType(t, c, wsutil.TypeGuessAck)
		}
	}
	waitForEnvelope(t, a, func(typ string) bool { return typ == wsutil.TypeGameOver })
	waitForEnvelope(t, b, func(typ string) bool { return typ == wsutil.TypeGameOver })

	saved := rs.waitSaved(t, 1)[0]
	if saved.TotalRounds != 2 || len(saved.Rounds) != 2 {
		t.Errorf("saved game shape wrong: rounds=%d total=%d", len(saved.Rounds), saved.TotalRounds)
	}
	if len(saved.Standings) != 2 || saved.Standings[0].Total < saved.Standings[1].Total {
		t.Errorf("standings wrong: %+v", saved.Standings)
	}
	if saved.Rounds[0].LocationID != "paris" || saved.Rounds[1].LocationID != "tokyo" {
		t.Errorf("location ids wrong: %q %q", saved.Rounds[0].LocationID, saved.Rounds[1].LocationID)
	}
	totalGuesses := 0
	for _, rnd := range saved.Rounds {
		if len(rnd.Results) != 2 {
			t.Errorf("round %d results = %d, want 2", rnd.Round, len(rnd.Results))
		}
		for _, res := range rnd.Results {
			if res.Guess != nil {
				totalGuesses++
			}
		}
	}
	if totalGuesses != 4 {
		t.Errorf("guesses persisted = %d, want 4", totalGuesses)
	}

	a.close(t)
	b.close(t)
}
