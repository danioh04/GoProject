package hub

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"time"

	"geoduel/internal/room"
)

const (
	codeLen      = 6
	idBytes      = 12
	maxAttempts  = 10
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

type Hub struct {
	logger     *slog.Logger
	maxPlayers int

	mu    sync.RWMutex
	rooms map[string]*room.Room
}

func New(logger *slog.Logger, maxPlayers int) *Hub {
	if maxPlayers <= 0 {
		maxPlayers = 8
	}
	return &Hub{
		logger:     logger,
		maxPlayers: maxPlayers,
		rooms:      make(map[string]*room.Room),
	}
}

func (h *Hub) Create(hostNickname string) *room.Room {
	h.mu.Lock()
	defer h.mu.Unlock()

	var code string
	for attempt := 1; ; attempt++ {
		if attempt > maxAttempts {
			h.logger.Error("join code space exhausted")
			code = strings.Repeat("X", codeLen)
			break
		}
		candidate := generateCode()
		if _, taken := h.rooms[candidate]; !taken {
			code = candidate
			break
		}
	}

	r := room.Start(generateID(), code, hostNickname, h.maxPlayers, h.logger)
	h.rooms[code] = r
	h.logger.Info("room created", "room_id", r.Snapshot().ID, "join_code", code)

	go func() {
		<-r.Done()
		h.mu.Lock()
		delete(h.rooms, code)
		h.mu.Unlock()
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

func generateCode() string {
	limit := 256 - 256%len(codeAlphabet)
	var out [codeLen]byte
	buf := make([]byte, 0, 32)
	for i := 0; i < codeLen; {
		if len(buf) == 0 {
			buf = make([]byte, 32)
			rand.Read(buf)
		}
		b := buf[0]
		buf = buf[1:]
		if int(b) < limit {
			out[i] = codeAlphabet[int(b)%len(codeAlphabet)]
			i++
		}
	}
	return string(out[:])
}

func generateID() string {
	var b [idBytes]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

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
