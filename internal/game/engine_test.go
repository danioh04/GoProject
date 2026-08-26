package game

import (
	"math"
	"reflect"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

func testLocations() []Location {
	return []Location{
		{ID: "paris", Lat: 48.8566, Lng: 2.3522},
		{ID: "tokyo", Lat: 35.6762, Lng: 139.6503},
		{ID: "sydney", Lat: -33.8688, Lng: 151.2093},
		{ID: "cairo", Lat: 30.0444, Lng: 31.2357},
		{ID: "lima", Lat: -12.0464, Lng: -77.0428},
	}
}

func newTestEngine(t *testing.T, mutate func(*Config)) (*Engine, *fakeClock) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Rounds = 3
	if mutate != nil {
		mutate(&cfg)
	}
	clock := &fakeClock{t: time.Unix(1700000000, 0).UTC()}
	locs := testLocations()
	pick := func(n int) []Location {
		out := make([]Location, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, locs[i%len(locs)])
		}
		return out
	}
	return New(cfg, clock.Now, pick), clock
}

func actionOfType[T Action](acts []Action) (T, bool) {
	for _, a := range acts {
		if v, ok := a.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

func joinOK(e *Engine, id PlayerID, nick string) []Action {
	acts := e.Apply(JoinEvent{PlayerID: id, Nickname: nick})
	if _, bad := actionOfType[RejectedAction](acts); bad {
		panic("join unexpectedly rejected in test setup")
	}
	return acts
}

func mustStart(t *testing.T, e *Engine, host PlayerID) []Action {
	t.Helper()
	acts := e.Apply(StartEvent{PlayerID: host})
	if _, bad := actionOfType[RejectedAction](acts); bad {
		t.Fatalf("start rejected: %+v", acts)
	}
	return acts
}

func TestJoinAssignsHostAndBuildsRoster(t *testing.T) {
	e, _ := newTestEngine(t, nil)

	first := joinOK(e, "p1", "alice")
	joined, ok := actionOfType[PlayerJoinedAction](first)
	if !ok || !joined.Host {
		t.Fatalf("first join should be host: %+v", first)
	}

	second := joinOK(e, "p2", "bob")
	joined, _ = actionOfType[PlayerJoinedAction](second)
	if joined.Host {
		t.Error("second join should not be host")
	}

	roster := e.Roster()
	if len(roster) != 2 || roster[0].Nickname != "alice" || !roster[0].IsHost {
		t.Errorf("roster wrong: %+v", roster)
	}
}

func TestJoinRejections(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.MaxPlayers = 2 })
	joinOK(e, "p1", "alice")

	if acts := e.Apply(JoinEvent{"pX", "ALICE"}); !isReject(acts, "nickname already taken") {
		t.Errorf("case-insensitive dup nickname not rejected: %+v", acts)
	}

	joinOK(e, "p2", "bob")
	if acts := e.Apply(JoinEvent{"p3", "carol"}); !isReject(acts, "room full") {
		t.Errorf("capacity not enforced: %+v", acts)
	}
}

func isReject(acts []Action, reasonSubstring string) bool {
	rej, ok := actionOfType[RejectedAction](acts)
	return ok && contains(rej.Reason, reasonSubstring)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestStartValidation(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.MinPlayers = 2 })
	joinOK(e, "p1", "alice")
	joinOK(e, "p2", "bob")

	if acts := e.Apply(StartEvent{"p2"}); !isReject(acts, "only the host") {
		t.Errorf("non-host start allowed: %+v", acts)
	}
	if acts := e.Apply(StartEvent{"ghost"}); !isReject(acts, "unknown player") {
		t.Errorf("unknown starter allowed: %+v", acts)
	}

	solo, _ := newTestEngine(t, nil)
	joinOK(solo, "p1", "alice")
	if acts := solo.Apply(StartEvent{"p1"}); !isReject(acts, "need at least") {
		t.Errorf("solo start allowed: %+v", acts)
	}
}

func TestFullMatchLifecycle(t *testing.T) {
	e, clock := newTestEngine(t, nil)
	joinOK(e, "a", "alice")
	joinOK(e, "b", "bob")

	startActs := mustStart(t, e, "a")
	if _, ok := actionOfType[MatchStartedAction](startActs); !ok {
		t.Fatalf("no MatchStarted: %+v", startActs)
	}
	rs, ok := actionOfType[RoundStartedAction](startActs)
	if !ok || rs.Round != 1 || rs.Location.ID != "paris" || rs.RoundSeconds != 60 {
		t.Fatalf("round 1 start wrong: %+v", rs)
	}
	timer, _ := actionOfType[TimerScheduledAction](startActs)
	if timer.Tag != (TimerTag{TimerDeadline, 1}) || timer.Delay != 60*time.Second {
		t.Fatalf("deadline timer wrong: %+v", timer)
	}
	if e.Phase() != PhasePlaying || e.Round() != 1 {
		t.Fatalf("phase/round = %s/%d", e.Phase(), e.Round())
	}

	totalAlice, totalBob := 0, 0

	guesses := []struct {
		near LatLng
		far  LatLng
	}{
		{LatLng{48.9, 2.4}, LatLng{35.7, 139.7}},
		{LatLng{35.7, 139.7}, LatLng{-33.9, 151.2}},
		{LatLng{-33.8, 151.3}, LatLng{-12.0, -77.0}},
	}

	for round := 1; round <= 3; round++ {
		target := testLocations()[round-1].LatLng()
		gA, gB := guesses[round-1].near, guesses[round-1].far

		actsA := e.Apply(GuessEvent{"a", gA})
		if _, ok := actionOfType[GuessAcceptedAction](actsA); !ok {
			t.Fatalf("round %d alice guess rejected: %+v", round, actsA)
		}
		if _, revealed := actionOfType[RoundRevealedAction](actsA); revealed {
			t.Fatalf("round %d revealed before everyone guessed", round)
		}

		actsB := e.Apply(GuessEvent{"b", gB})
		reveal, ok := actionOfType[RoundRevealedAction](actsB)
		if !ok {
			t.Fatalf("round %d early reveal missing after last guess: %+v", round, actsB)
		}
		if reveal.Round != round || reveal.Target != target {
			t.Fatalf("reveal %d wrong: target %+v round %d", round, reveal.Target, reveal.Round)
		}
		if len(reveal.Results) != 2 || reveal.Results[0].Score < reveal.Results[1].Score {
			t.Fatalf("results unsorted or wrong size: %+v", reveal.Results)
		}
		rtimer, _ := actionOfType[TimerScheduledAction](actsB)
		if rtimer.Tag.Kind != TimerReveal || rtimer.Tag.Round != round {
			t.Fatalf("reveal timer wrong: %+v", rtimer)
		}

		for _, res := range reveal.Results {
			want := Score(*res.Guess, target, e.cfg.MaxScore)
			if res.Score != want {
				t.Errorf("round %d %s score = %d, want %d", round, res.Nickname, res.Score, want)
			}
			switch res.PlayerID {
			case "a":
				totalAlice += res.Score
			case "b":
				totalBob += res.Score
			}
		}
		if e.Phase() != PhaseReveal {
			t.Fatalf("post-reveal phase = %s", e.Phase())
		}

		clock.advance(10 * time.Second)
		next := e.Apply(TimeoutEvent{TimerTag{TimerReveal, round}})
		if round < 3 {
			nrs, ok := actionOfType[RoundStartedAction](next)
			if !ok || nrs.Round != round+1 {
				t.Fatalf("advance to round %d failed: %+v", round+1, next)
			}
			continue
		}

		end, ok := actionOfType[MatchEndedAction](next)
		if !ok {
			t.Fatalf("match did not end after final reveal: %+v", next)
		}
		if len(end.Standings) != 2 {
			t.Fatalf("standings size = %d", len(end.Standings))
		}
		if end.Standings[0].Total < end.Standings[1].Total {
			t.Errorf("standings unsorted: %+v", end.Standings)
		}
		for _, s := range end.Standings {
			switch s.PlayerID {
			case "a":
				if s.Total != totalAlice {
					t.Errorf("alice total = %d, want %d", s.Total, totalAlice)
				}
			case "b":
				if s.Total != totalBob {
					t.Errorf("bob total = %d, want %d", s.Total, totalBob)
				}
			}
		}
	}

	if e.Phase() != PhaseFinished {
		t.Errorf("final phase = %s", e.Phase())
	}
}

func TestLastWriteWinsGuess(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.Rounds = 1 })
	joinOK(e, "a", "alice")
	joinOK(e, "b", "bob")
	mustStart(t, e, "a")

	e.Apply(GuessEvent{"a", LatLng{10, 10}})
	e.Apply(GuessEvent{"a", LatLng{48.85, 2.35}})

	acts := e.Apply(GuessEvent{"b", LatLng{48.87, 2.37}})
	reveal, ok := actionOfType[RoundRevealedAction](acts)
	if !ok {
		t.Fatal("no reveal")
	}
	var alice RoundResult
	for _, r := range reveal.Results {
		if r.PlayerID == "a" {
			alice = r
		}
	}
	if alice.Guess == nil || *alice.Guess != (LatLng{48.85, 2.35}) {
		t.Errorf("LWW violated, scored guess = %+v", alice.Guess)
	}
}

func TestDeadlineRevealsWithZeroScoresForMissing(t *testing.T) {
	e, clock := newTestEngine(t, func(c *Config) { c.Rounds = 1 })
	joinOK(e, "a", "alice")
	joinOK(e, "b", "bob")
	mustStart(t, e, "a")

	if acts := e.Apply(GuessEvent{"a", LatLng{48.85, 2.35}}); len(acts) == 0 {
		t.Fatal("guess rejected")
	}

	clock.advance(61 * time.Second)
	acts := e.Apply(TimeoutEvent{TimerTag{TimerDeadline, 1}})
	reveal, ok := actionOfType[RoundRevealedAction](acts)
	if !ok {
		t.Fatal("deadline did not trigger reveal")
	}
	var bob RoundResult
	for _, r := range reveal.Results {
		if r.PlayerID == "b" {
			bob = r
		}
	}
	if bob.Guess != nil || bob.Score != 0 {
		t.Errorf("missing guess should score zero: %+v", bob)
	}
	if bob.Nickname != "bob" {
		t.Errorf("missing player absent from results: %+v", reveal.Results)
	}
}

func TestStaleTimeoutIgnored(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.Rounds = 2 })
	joinOK(e, "a", "alice")
	joinOK(e, "b", "bob")
	mustStart(t, e, "a")

	if acts := e.Apply(TimeoutEvent{TimerTag{TimerDeadline, 99}}); acts != nil {
		t.Errorf("stale deadline acted: %+v", acts)
	}
	if acts := e.Apply(TimeoutEvent{TimerTag{TimerReveal, 1}}); acts != nil {
		t.Errorf("reveal timeout during playing acted: %+v", acts)
	}
	if e.Phase() != PhasePlaying {
		t.Errorf("phase changed by stale timers: %s", e.Phase())
	}
}

func TestGuessValidationAndPhaseGuards(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	joinOK(e, "a", "alice")

	if acts := e.Apply(GuessEvent{"a", LatLng{48.85, 2.35}}); !isReject(acts, "not accepting") {
		t.Errorf("guess allowed in lobby: %+v", acts)
	}

	joinOK(e, "b", "bob")
	mustStart(t, e, "a")

	bad := []LatLng{{91, 0}, {0, 200}, {math.NaN(), 0}}
	for _, p := range bad {
		if acts := e.Apply(GuessEvent{"a", p}); !isReject(acts, "invalid coordinates") {
			t.Errorf("bad coords %+v accepted: %+v", p, acts)
		}
	}
	if acts := e.Apply(GuessEvent{"ghost", LatLng{0, 0}}); !isReject(acts, "unknown player") {
		t.Errorf("ghost guess allowed: %+v", acts)
	}

	e.Apply(GuessEvent{"a", LatLng{48.85, 2.35}})
	e.Apply(GuessEvent{"b", LatLng{51.5, -0.12}})
	if acts := e.Apply(GuessEvent{"a", LatLng{0, 0}}); !isReject(acts, "not accepting") {
		t.Errorf("guess allowed during reveal: %+v", acts)
	}
}

func TestLeaveSemantics(t *testing.T) {
	t.Run("promotes earliest remaining as host", func(t *testing.T) {
		e, _ := newTestEngine(t, nil)
		joinOK(e, "a", "alice")
		joinOK(e, "b", "bob")
		joinOK(e, "c", "carol")

		e.Apply(LeaveEvent{"a"})
		for _, v := range e.Roster() {
			if v.PlayerID == "a" {
				t.Fatal("departed player still in roster")
			}
			wantHost := v.PlayerID == "b"
			if v.IsHost != wantHost {
				t.Errorf("%s host = %v", v.PlayerID, v.IsHost)
			}
		}
	})

	t.Run("empty room emits RoomEmpty", func(t *testing.T) {
		e, _ := newTestEngine(t, nil)
		joinOK(e, "a", "alice")
		joinOK(e, "b", "bob")

		mid := e.Apply(LeaveEvent{"a"})
		if _, ok := actionOfType[RoomEmptyAction](mid); ok {
			t.Fatal("RoomEmpty with a player remaining")
		}
		last := e.Apply(LeaveEvent{"b"})
		if _, ok := actionOfType[RoomEmptyAction](last); !ok {
			t.Errorf("expected RoomEmpty when last player left: %+v", last)
		}
	})

	t.Run("holdout leaving triggers early reveal", func(t *testing.T) {
		e, _ := newTestEngine(t, func(c *Config) { c.Rounds = 2 })
		joinOK(e, "a", "alice")
		joinOK(e, "b", "bob")
		mustStart(t, e, "a")

		e.Apply(GuessEvent{"a", LatLng{48.85, 2.35}})
		acts := e.Apply(LeaveEvent{"b"})
		reveal, ok := actionOfType[RoundRevealedAction](acts)
		if !ok {
			t.Fatalf("expected early reveal after holdout left: %+v", acts)
		}
		if len(reveal.Results) != 1 || reveal.Results[0].PlayerID != "a" {
			t.Errorf("reveal should contain only remaining player: %+v", reveal.Results)
		}
	})
}

func TestJoinAfterMatchStartedRejected(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	joinOK(e, "a", "alice")
	joinOK(e, "b", "bob")
	mustStart(t, e, "a")

	if acts := e.Apply(JoinEvent{"c", "carol"}); !isReject(acts, "match already started") {
		t.Errorf("late join allowed: %+v", acts)
	}
	if e.PlayerCount() != 2 {
		t.Errorf("player count = %d, want 2", e.PlayerCount())
	}
}

func TestStandingsTiebreakByJoinOrder(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.Rounds = 1 })
	joinOK(e, "a", "alice")
	joinOK(e, "b", "bob")
	mustStart(t, e, "a")

	target := LatLng{48.8566, 2.3522}
	e.Apply(GuessEvent{"a", target})
	e.Apply(GuessEvent{"b", target})

	acts := e.Apply(TimeoutEvent{TimerTag{TimerReveal, 1}})
	end, ok := actionOfType[MatchEndedAction](acts)
	if !ok {
		t.Fatal("no match end")
	}
	if len(end.Standings) != 2 || end.Standings[0].PlayerID != "a" || end.Standings[1].PlayerID != "b" {
		t.Errorf("tie not broken by join order: %+v", end.Standings)
	}
	if !reflect.DeepEqual(end.Standings[0], Standing{"a", "alice", e.cfg.MaxScore}) {
		t.Errorf("standing wrong: %+v", end.Standings[0])
	}
}
