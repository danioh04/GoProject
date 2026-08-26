package room_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"geoduel/internal/room"
	"geoduel/internal/wsutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLifecycle(t *testing.T) {
	r := room.Start("room-1", "ABC234", "dan", 8, testLogger())

	snap := r.Snapshot()
	if snap.ID != "room-1" || snap.JoinCode != "ABC234" || snap.HostNickname != "dan" {
		t.Errorf("snapshot identity mismatch: %+v", snap)
	}
	if snap.State != room.PhaseLobby || snap.PlayerCount != 0 {
		t.Errorf("initial snapshot = state %q count %d, want lobby/0", snap.State, snap.PlayerCount)
	}

	r.Close()
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("room did not terminate within 2s of Close")
	}

	if got := r.Snapshot().State; got != room.PhaseClosed {
		t.Errorf("state after close = %q, want %q", got, room.PhaseClosed)
	}

	r.Close()
}

func TestSnapshotIsCopySafe(t *testing.T) {
	r := room.Start("id", "XYZ789", "host", 8, testLogger())
	defer r.Close()

	snap := r.Snapshot()
	snap.State = "tampered"
	if r.Snapshot().State == "tampered" {
		t.Error("Snapshot returned shared mutable state")
	}
}

func TestNotifyAfterCloseIsSafe(t *testing.T) {
	r := room.Start("id", "XYZ789", "host", 8, testLogger())
	r.Close()
	<-r.Done()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.NotifyInbound("ghost", wsutil.Envelope{Version: 1, Type: wsutil.TypePing})
		r.NotifyDisconnect("ghost")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify calls blocked after Close")
	}
}
