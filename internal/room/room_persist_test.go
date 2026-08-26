package room_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"geoduel/internal/game"
	"geoduel/internal/room"
	"geoduel/internal/wsutil"
)

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
			t.Errorf("round %d results = %d, want 2", rnd.Number, len(rnd.Results))
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
