package hub

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"geoduel/internal/room"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestCreateGeneratesUniqueCodesAndIDs(t *testing.T) {
	h := New(testLogger())
	const n = 1000

	codes := make(map[string]struct{}, n)
	ids := make(map[string]struct{}, n)
	rooms := make([]*room.Room, 0, n)

	for i := 0; i < n; i++ {
		r := h.Create("player")
		rooms = append(rooms, r)

		code := r.Snapshot().JoinCode
		if len(code) != codeLen {
			t.Fatalf("code length = %d, want %d", len(code), codeLen)
		}
		for _, c := range code {
			if !strings.ContainsRune(codeAlphabet, c) {
				t.Fatalf("code %q contains invalid character %q", code, c)
			}
		}
		if _, dup := codes[code]; dup {
			t.Fatalf("duplicate join code %q after %d creations", code, i+1)
		}
		codes[code] = struct{}{}

		id := r.Snapshot().ID
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate room id %q", id)
		}
		ids[id] = struct{}{}
	}

	h.Shutdown(2 * time.Second)
}

func TestGetIsCaseInsensitiveAndNormalizes(t *testing.T) {
	h := New(testLogger())
	defer h.Shutdown(2 * time.Second)

	want := h.Create("host")
	code := want.Snapshot().JoinCode

	got, ok := h.Get(strings.ToLower(strings.TrimSpace(code + "  ")))
	if !ok {
		t.Fatalf("Get(%q) not found", code)
	}
	if got.Snapshot().JoinCode != want.Snapshot().JoinCode {
		t.Errorf("Get returned room %q, want %q", got.Snapshot().JoinCode, code)
	}

	if _, ok := h.Get("ZZZZZZ"); ok {
		t.Error("Get returned a room for unknown code")
	}
}

func TestShutdownClosesAllRooms(t *testing.T) {
	h := New(testLogger())

	var rooms []*room.Room
	for i := 0; i < 20; i++ {
		rooms = append(rooms, h.Create("host"))
	}

	closed := h.Shutdown(2 * time.Second)
	if closed != len(rooms) {
		t.Errorf("Shutdown reported %d closed, want %d", closed, len(rooms))
	}
	for _, r := range rooms {
		select {
		case <-r.Done():
		default:
			t.Errorf("room %s still running after Shutdown", r.Snapshot().JoinCode)
		}
		if r.Snapshot().State != room.PhaseClosed {
			t.Errorf("room %s state = %q, want closed", r.Snapshot().JoinCode, r.Snapshot().State)
		}
	}
}
