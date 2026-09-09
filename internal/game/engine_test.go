package game

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct {
	current time.Time
}

// Now returns the simulated current time for testing.
func (f *fakeClock) Now() time.Time {
	return f.current
}

// Advance moves the simulated clock forward by the given duration.
func (f *fakeClock) Advance(d time.Duration) {
	f.current = f.current.Add(d)
}

// dummyPicker constructs a deterministic location picker returning slice copies or synthetic coordinates.
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

// TestMatch_DeterministicPlaythrough_FakeClock tests a complete two-round match deterministically.
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

	var endAct GameEndedAction
	for _, a := range acts {
		if e, ok := a.(GameEndedAction); ok {
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

// TestMatch_SoloPlayerForfeitsWhenOpponentLeaves verifies that a match finishes when a player disconnects.
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
		if me, ok := a.(GameEndedAction); ok {
			matchEnded = true
			if me.Reason != "insufficient_players" {
				t.Fatalf("expected insufficient_players reason, got %s", me.Reason)
			}
		}
	}
	if !matchEnded {
		t.Fatalf("expected GameEndedAction on opponent leave")
	}
}

// BenchmarkMatch_FullPlaythrough_FakeClock measures the throughput and allocations of a complete five-round match.
func BenchmarkMatch_FullPlaythrough_FakeClock(b *testing.B) {
	locs := []Location{
		{ID: "loc1", PanoID: "p1", LatLng: LatLng{Lat: 48.8566, Lng: 2.3522}},
		{ID: "loc2", PanoID: "p2", LatLng: LatLng{Lat: 35.6762, Lng: 139.6503}},
		{ID: "loc3", PanoID: "p3", LatLng: LatLng{Lat: 40.7128, Lng: -74.0060}},
		{ID: "loc4", PanoID: "p4", LatLng: LatLng{Lat: -33.8688, Lng: 151.2093}},
		{ID: "loc5", PanoID: "p5", LatLng: LatLng{Lat: 51.5074, Lng: -0.1278}},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		clock := &fakeClock{current: time.Now()}
		cfg := Config{
			Rounds:     5,
			RoundTime:  60 * time.Second,
			RevealTime: 10 * time.Second,
			MaxPlayers: 4,
			MinPlayers: 2,
			MaxScore:   5000,
			Clock:      clock,
		}

		engine := New(cfg, dummyPicker(locs...))
		engine.Apply(JoinEvent{PlayerID: "p1", Nickname: "Alice"})
		engine.Apply(JoinEvent{PlayerID: "p2", Nickname: "Bob"})
		engine.Apply(StartEvent{PlayerID: "p1"})

		for r := 1; r <= 5; r++ {
			engine.Apply(GuessEvent{PlayerID: "p1", Guess: LatLng{Lat: 48.85, Lng: 2.35}})
			engine.Apply(GuessEvent{PlayerID: "p2", Guess: LatLng{Lat: 35.67, Lng: 139.65}})
			clock.Advance(10 * time.Second)
			engine.Apply(TimeoutEvent{Tag: TimerTag{Kind: TimerReveal, Round: r}})
		}
	}
}
