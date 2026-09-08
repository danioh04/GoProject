package game

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct {
	current time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.current
}

func (f *fakeClock) Advance(d time.Duration) {
	f.current = f.current.Add(d)
}

func dummyPicker(locations ...Location) func(int) []Location {
	return func(n int) []Location {
		if len(locations) >= n {
			return locations[:n]
		}
		out := make([]Location, n)
		for i := 0; i < n; i++ {
			out[i] = Location{
				ID:     fmt.Sprintf("loc-%d", i+1),
				PanoID: fmt.Sprintf("pano-%d", i+1),
				LatLng: LatLng{Lat: float64(i * 10), Lng: float64(i * 10)},
			}
		}
		return out
	}
}

func TestLobby_JoinAndCapacity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxPlayers = 3
	engine := New(cfg, dummyPicker())

	// Join player 1
	acts := engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Alice"})
	if len(acts) < 2 {
		t.Fatalf("expected at least 2 actions on join, got %d", len(acts))
	}
	p1Joined, ok := acts[0].(PlayerJoinedAction)
	if !ok || !p1Joined.Host {
		t.Fatalf("expected p1 to be host, got %+v", acts[0])
	}

	// Join player 2
	acts = engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "Bob"})
	p2Joined, ok := acts[0].(PlayerJoinedAction)
	if !ok || p2Joined.Host {
		t.Fatalf("expected p2 to not be host, got %+v", acts[0])
	}

	// Join player 3
	acts = engine.Apply(JoinEvent{PlayerID: "p3", Nickname: "Charlie"})
	if _, ok := acts[0].(PlayerJoinedAction); !ok {
		t.Fatalf("expected p3 to join successfully")
	}

	// Join player 4 (exceeds capacity)
	acts = engine.Apply(JoinEvent{PlayerID: "p4", Nickname: "Dave"})
	rej, ok := acts[0].(RejectedAction)
	if !ok || rej.Reason != "room full" {
		t.Fatalf("expected rejection for full room, got %+v", acts[0])
	}
}

func TestLobby_CaseInsensitiveDuplicateNickname(t *testing.T) {
	cfg := DefaultConfig()
	engine := New(cfg, dummyPicker())

	engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Alice"})
	acts := engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "aLiCe"})
	rej, ok := acts[0].(RejectedAction)
	if !ok || rej.Reason != "nickname already taken" {
		t.Fatalf("expected nickname rejection, got %+v", acts[0])
	}
}

func TestLobby_HostAssignment_Creator(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CreatorNick = "HostUser"
	engine := New(cfg, dummyPicker())

	// Bob joins first
	engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Bob"})
	roster := engine.Roster()
	if len(roster) != 1 || !roster[0].IsHost {
		t.Fatalf("expected Bob to be temporary host")
	}

	// HostUser joins second: should become host
	engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "HostUser"})
	roster = engine.Roster()
	if len(roster) != 2 {
		t.Fatalf("expected 2 players")
	}
	for _, p := range roster {
		if p.Nickname == "HostUser" && !p.IsHost {
			t.Fatalf("expected HostUser to be host")
		}
		if p.Nickname == "Bob" && p.IsHost {
			t.Fatalf("expected Bob to lose host status to creator")
		}
	}
}

func TestLobby_StartRequirements(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinPlayers = 2
	engine := New(cfg, dummyPicker())

	engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Alice"})

	// Solo player cannot start
	acts := engine.Apply(StartEvent{PlayerID: "p1"})
	rej, ok := acts[0].(RejectedAction)
	if !ok || rej.Reason != "need at least 2 players to start" {
		t.Fatalf("expected start rejection for < min players, got %+v", acts[0])
	}

	// Non-host cannot start
	engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "Bob"})
	acts = engine.Apply(StartEvent{PlayerID: "p2"})
	rej, ok = acts[0].(RejectedAction)
	if !ok || rej.Reason != "only the host can start the match" {
		t.Fatalf("expected rejection for non-host start, got %+v", acts[0])
	}

	// Host starts successfully
	acts = engine.Apply(StartEvent{PlayerID: "p1"})
	if len(acts) < 3 {
		t.Fatalf("expected match start actions, got %d", len(acts))
	}
	if _, ok := acts[0].(MatchStartedAction); !ok {
		t.Fatalf("expected MatchStartedAction")
	}
	if _, ok := acts[1].(RoundStartedAction); !ok {
		t.Fatalf("expected RoundStartedAction")
	}
	if engine.Phase() != PhasePlaying {
		t.Fatalf("expected PhasePlaying, got %s", engine.Phase())
	}
}

func TestMatch_DeterministicPlaythrough_FakeClock(t *testing.T) {
	start := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{current: start}

	cfg := Config{
		Rounds:     2,
		RoundTime:  30 * time.Second,
		RevealTime: 5 * time.Second,
		MaxPlayers: 4,
		MinPlayers: 2,
		MaxScore:   5000,
		Clock:      clock,
	}

	locs := []Location{
		{ID: "loc1", PanoID: "pano1", LatLng: LatLng{Lat: 48.8566, Lng: 2.3522}},   // Paris
		{ID: "loc2", PanoID: "pano2", LatLng: LatLng{Lat: 35.6762, Lng: 139.6503}}, // Tokyo
	}

	engine := New(cfg, dummyPicker(locs...))

	engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Alice"})
	engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "Bob"})

	// Start match
	acts := engine.Apply(StartEvent{PlayerID: "p1"})
	rsAct, ok := acts[1].(RoundStartedAction)
	if !ok || rsAct.Round != 1 || rsAct.Deadline != start.Add(30*time.Second) {
		t.Fatalf("round 1 started deadline mismatch: got %v", rsAct.Deadline)
	}

	// Round 1: Alice guesses near Paris, Bob guesses further
	acts = engine.Apply(GuessEvent{
		PlayerID: "p1",
		Guess:    LatLng{Lat: 48.86, Lng: 2.35}, // ~400 meters away
	})
	if _, ok := acts[0].(GuessAcceptedAction); !ok {
		t.Fatalf("expected GuessAcceptedAction")
	}
	// Alice guessed, but Bob hasn't: round should NOT reveal yet
	if engine.Phase() != PhasePlaying {
		t.Fatalf("expected PhasePlaying while waiting for Bob")
	}

	// Bob guesses
	acts = engine.Apply(GuessEvent{
		PlayerID: "p2",
		Guess:    LatLng{Lat: 51.5074, Lng: -0.1278}, // London (~343 km)
	})
	// Now both guessed: round reveals immediately!
	if engine.Phase() != PhaseReveal {
		t.Fatalf("expected PhaseReveal after all guessed, got %s", engine.Phase())
	}
	var revAct RoundRevealedAction
	for _, a := range acts {
		if r, ok := a.(RoundRevealedAction); ok {
			revAct = r
			break
		}
	}
	if revAct.Round != 1 {
		t.Fatalf("expected round 1 revealed")
	}
	if len(revAct.Results) != 2 {
		t.Fatalf("expected 2 results")
	}
	// Alice should be first with ~5000 points
	if revAct.Results[0].PlayerID != "p1" || revAct.Results[0].Score < 4990 {
		t.Fatalf("expected Alice to win round 1 with top score: %+v", revAct.Results[0])
	}

	// Advance clock by 5s reveal delay -> trigger TimeoutEvent for reveal
	clock.Advance(5 * time.Second)
	_ = engine.Apply(TimeoutEvent{Tag: TimerTag{Kind: TimerReveal, Round: 1}})
	if engine.Phase() != PhasePlaying {
		t.Fatalf("expected round 2 PhasePlaying, got %s", engine.Phase())
	}

	// Round 2: Timeout without guesses
	clock.Advance(30 * time.Second)
	_ = engine.Apply(TimeoutEvent{Tag: TimerTag{Kind: TimerDeadline, Round: 2}})
	if engine.Phase() != PhaseReveal {
		t.Fatalf("expected PhaseReveal on timeout, got %s", engine.Phase())
	}

	// Advance reveal timer -> should trigger MatchEndedAction (since Rounds=2)
	clock.Advance(5 * time.Second)
	acts = engine.Apply(TimeoutEvent{Tag: TimerTag{Kind: TimerReveal, Round: 2}})
	if engine.Phase() != PhaseFinished {
		t.Fatalf("expected PhaseFinished, got %s", engine.Phase())
	}
	var endAct MatchEndedAction
	for _, a := range acts {
		if e, ok := a.(MatchEndedAction); ok {
			endAct = e
			break
		}
	}
	if len(endAct.Standings) != 2 {
		t.Fatalf("expected 2 standings")
	}
	if endAct.Standings[0].PlayerID != "p1" {
		t.Fatalf("expected Alice to win the match: %+v", endAct.Standings)
	}
}

func TestMatch_SoloPlayerForfeitsWhenOpponentLeaves(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinPlayers = 2
	engine := New(cfg, dummyPicker())

	engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Alice"})
	engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "Bob"})
	engine.Apply(StartEvent{PlayerID: "p1"})

	// Bob disconnects during round 1
	acts := engine.Apply(LeaveEvent{PlayerID: "p2"})
	if engine.Phase() != PhaseFinished {
		t.Fatalf("expected match to finish when opponent leaves, got %s", engine.Phase())
	}

	var matchEnded bool
	for _, a := range acts {
		if me, ok := a.(MatchEndedAction); ok {
			matchEnded = true
			if me.Reason != "insufficient_players" {
				t.Fatalf("expected insufficient_players reason, got %s", me.Reason)
			}
		}
	}
	if !matchEnded {
		t.Fatalf("expected MatchEndedAction on opponent leave")
	}
}
