package room_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"geoduel/internal/game"
	"geoduel/internal/room"
	"geoduel/internal/wsutil"
)

func matchLocations() []game.Location {
	return []game.Location{
		{ID: "paris", Lat: 48.8566, Lng: 2.3522},
		{ID: "tokyo", Lat: 35.6762, Lng: 139.6503},
	}
}

func testPicker(n int) []game.Location {
	locs := matchLocations()
	if n > len(locs) {
		n = len(locs)
	}
	return locs[:n]
}

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
	tokenA := mustReadToken(t, a)
	a.readEnvelope(t)

	b := dial("bob")
	tokenB := mustReadToken(t, b)
	b.readEnvelope(t)
	a.readEnvelope(t)

	a.send(t, wsutil.Envelope{Version: 1, Type: wsutil.TypeStartGame, Token: tokenA})
	assertType(t, a, wsutil.TypeGameStart)
	rs1 := assertType(t, a, wsutil.TypeRoundStart)
	assertRoundStartHidesTarget(t, rs1, "paris")
	assertType(t, b, wsutil.TypeGameStart)
	assertType(t, b, wsutil.TypeRoundStart)

	b.send(t, guessEnvelope(tokenB, 48.8566, 2.3522))
	assertType(t, b, wsutil.TypeGuessAck)

	a.send(t, guessEnvelope(tokenA, 0, 0))
	assertType(t, a, wsutil.TypeGuessAck)

	verifyFirstReveal(t, assertType(t, a, wsutil.TypeRoundResult))
	assertType(t, b, wsutil.TypeRoundResult)

	rs2 := waitForRoundStart(t, a, 2)
	if got := extractLocationID(t, rs2); got != "tokyo" {
		t.Errorf("round 2 location = %q, want tokyo", got)
	}
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

func guessEnvelope(token string, lat, lng float64) wsutil.Envelope {
	env, _ := wsutil.NewEnvelope(wsutil.TypeGuess, map[string]float64{"lat": lat, "lng": lng})
	env.Token = token
	return env
}

func mustReadToken(t *testing.T, c *testClient) string {
	t.Helper()
	env := c.readEnvelope(t)
	if env.Type != wsutil.TypeJoined {
		t.Fatalf("expected joined, got %s", env.Type)
	}
	var p struct {
		Token string `json:"token"`
	}
	json.Unmarshal(env.Payload, &p)
	return p.Token
}

func assertType(t *testing.T, c *testClient, want string) wsutil.Envelope {
	t.Helper()
	env := c.readEnvelope(t)
	if env.Type != want {
		t.Fatalf("expected %s, got %s (%s)", want, env.Type, string(env.Payload))
	}
	return env
}

func assertRoundStartHidesTarget(t *testing.T, env wsutil.Envelope, wantLocID string) {
	t.Helper()
	raw := string(env.Payload)
	if !strings.Contains(raw, `"id":"`+wantLocID+`"`) {
		t.Errorf("round_start missing location id %q: %s", wantLocID, raw)
	}
	if strings.Contains(raw, `"lat"`) || strings.Contains(raw, `"lng"`) || strings.Contains(raw, "target") {
		t.Errorf("ROUND START LEAKS TARGET COORDINATES: %s", raw)
	}
}

func extractLocationID(t *testing.T, env wsutil.Envelope) string {
	t.Helper()
	var p struct {
		Location game.LocationRef `json:"location"`
	}
	json.Unmarshal(env.Payload, &p)
	return p.Location.ID
}

func verifyFirstReveal(t *testing.T, env wsutil.Envelope) {
	t.Helper()
	var p struct {
		Round   int                `json:"round"`
		Target  game.LatLng        `json:"target"`
		Results []game.RoundResult `json:"results"`
	}
	json.Unmarshal(env.Payload, &p)
	if p.Round != 1 {
		t.Errorf("reveal round = %d", p.Round)
	}
	if p.Target.Lat != 48.8566 || p.Target.Lng != 2.3522 {
		t.Errorf("revealed target wrong: %+v", p.Target)
	}
	if len(p.Results) != 2 {
		t.Fatalf("results size = %d", len(p.Results))
	}
	top := p.Results[0]
	if top.Score != 5000 || top.Guess == nil {
		t.Errorf("exact guess should score 5000 first: %+v", top)
	}
	if p.Results[1].Score <= 0 || p.Results[1].Score >= 5000 {
		t.Errorf("far guess score out of range: %+v", p.Results[1])
	}
}

func verifyEveryoneMissed(t *testing.T, env wsutil.Envelope) {
	t.Helper()
	var p struct {
		Round   int                `json:"round"`
		Results []game.RoundResult `json:"results"`
	}
	json.Unmarshal(env.Payload, &p)
	if p.Round != 2 {
		t.Errorf("reveal round = %d, want 2", p.Round)
	}
	for _, res := range p.Results {
		if res.Guess != nil || res.Score != 0 {
			t.Errorf("expected zero-score missed guess: %+v", res)
		}
	}
}

func waitForRoundStart(t *testing.T, c *testClient, round int) wsutil.Envelope {
	t.Helper()
	for i := 0; i < 10; i++ {
		env := c.readEnvelope(t)
		if env.Type == wsutil.TypeRoundStart {
			var p struct {
				Round int `json:"round"`
			}
			json.Unmarshal(env.Payload, &p)
			if p.Round == round {
				return env
			}
		}
	}
	t.Fatal("round start never arrived")
	return wsutil.Envelope{}
}

func drainUntil(t *testing.T, c *testClient, stopAt string) {
	for i := 0; i < 10; i++ {
		env := c.readEnvelope(t)
		if env.Type == stopAt {
			return
		}
	}
}
