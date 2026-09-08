package room

import (
	"prism/internal/game"
	"testing"
)

func TestValidJoinCode(t *testing.T) {
	tests := []struct {
		code  string
		valid bool
	}{
		{"ABCDEF", true},
		{"234567", true},
		{"ABCDE", false},   // too short
		{"ABCDEFG", false}, // too long
		{"ABCD0F", false},  // contains '0' (excluded from alphabet)
		{"ABCD1F", false},  // contains '1' (excluded from alphabet)
		{"abc234", true},   // case insensitive
	}

	for _, tt := range tests {
		got := ValidJoinCode(tt.code)
		if got != tt.valid {
			t.Errorf("ValidJoinCode(%q) = %v; want %v", tt.code, got, tt.valid)
		}
	}
}

func TestHub_CreateAndGet(t *testing.T) {
	opts := Options{
		MaxPlayers: 4,
		Config:     game.DefaultConfig(),
		Picker: func(n int) []game.Location {
			return make([]game.Location, n)
		},
	}
	hub := NewHub(nil, opts)

	r := hub.Create("Creator")
	snap := r.Snapshot()
	if snap.ID == "" || snap.JoinCode == "" {
		t.Fatalf("expected non-empty ID and join code: %+v", snap)
	}
	if snap.HostNickname != "Creator" {
		t.Fatalf("expected HostNickname 'Creator', got %q", snap.HostNickname)
	}

	found, ok := hub.Get(snap.JoinCode)
	if !ok || found != r {
		t.Fatalf("expected to look up created room by code")
	}

	r.Close()
	<-r.Done()
}

func TestRoom_ConcurrentDeliverAndClose(t *testing.T) {
	opts := Options{
		MaxPlayers: 4,
		Config:     game.DefaultConfig(),
		Picker: func(n int) []game.Location {
			return make([]game.Location, n)
		},
	}
	hub := NewHub(nil, opts)
	r := hub.Create("Host")

	// Fire concurrent delivers while closing the room
	done := make(chan struct{})
	for range 10 {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					r.NotifyInbound("player-1", Envelope{Version: 1, Type: "guess"})
				}
			}
		}()
	}

	r.Close()
	close(done)
	<-r.Done()
}
