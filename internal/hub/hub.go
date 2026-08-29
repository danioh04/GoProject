// Package hub maps join codes to active room actors.
package hub

import (
	"crypto/rand"
	"encoding/hex"
	"geoduel/internal/metrics"
	"geoduel/internal/room"
	"log/slog"
	mrand "math/rand/v2"
	"strings"
	"sync"
	"time"
)

const (
	codeLen      = 6
	idBytes      = 12
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

type Hub struct {
	logger   *slog.Logger
	defaults room.Options

	mu    sync.RWMutex
	rooms map[string]*room.Room
}

func New(logger *slog.Logger, defaults room.Options) *Hub {
	if defaults.MaxPlayers <= 0 {
		defaults.MaxPlayers = 8
	}
	return &Hub{
		logger:   logger,
		defaults: defaults,
		rooms:    make(map[string]*room.Room),
	}
}

func (h *Hub) Create(hostNickname string) *room.Room {
	h.mu.Lock()
	defer h.mu.Unlock()

	var code string
	for {
		code = generateCode()
		if _, taken := h.rooms[code]; !taken {
			break
		}
	}

	opts := h.defaults
	opts.ID = generateID()
	opts.JoinCode = code
	opts.Label = hostNickname
	opts.Logger = h.logger
	r := room.Start(opts)
	h.rooms[code] = r
	metrics.RoomsActive.Add(1)
	h.logger.Info("room created", "room_id", r.Snapshot().ID, "join_code", code)

	go func() {
		<-r.Done()
		h.mu.Lock()
		delete(h.rooms, code)
		h.mu.Unlock()
		metrics.RoomsActive.Add(-1)
		h.logger.Info("room removed", "join_code", code)
	}()
	return r
}

func (h *Hub) Get(code string) (*room.Room, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r, ok := h.rooms[strings.ToUpper(strings.TrimSpace(code))]
	return r, ok
}

func (h *Hub) Shutdown(wait time.Duration) int {
	h.mu.RLock()
	rooms := make([]*room.Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.RUnlock()

	for _, r := range rooms {
		r.Close()
	}

	deadline := time.Now().Add(wait)
	closed := 0
	for _, r := range rooms {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			h.logger.Warn("room shutdown timed out")
			return closed
		}
		timer := time.NewTimer(remaining)
		select {
		case <-r.Done():
			timer.Stop()
			closed++
		case <-timer.C:
			h.logger.Warn("room shutdown timed out", "join_code", r.Snapshot().JoinCode)
			return closed
		}
	}
	return closed
}

// ValidJoinCode reports whether code matches the expected join code format and charset.
func ValidJoinCode(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != codeLen {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune(codeAlphabet, c) {
			return false
		}
	}
	return true
}

func generateCode() string {
	var out [codeLen]byte
	for i := range out {
		out[i] = codeAlphabet[mrand.IntN(len(codeAlphabet))]
	}
	return string(out[:])
}

func generateID() string {
	var b [idBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")[:24]
	}
	return hex.EncodeToString(b[:])
}
