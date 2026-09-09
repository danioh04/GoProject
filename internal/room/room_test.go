package room

import (
	"scope/internal/game"
	"testing"
)

// TestRoom_ConcurrentDeliverAndClose tests concurrent message deliveries during room shutdown.
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
