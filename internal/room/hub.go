package room

import (
	"log/slog"
	mrand "math/rand/v2"
	"strings"
	"sync"
	"time"
)

const (
	codeLen            = 6
	idBytes            = 12
	codeAlphabet       = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	lobbyInactivityTTL = 5 * time.Minute
)

type Hub struct {
	logger   *slog.Logger
	defaults Options

	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewHub(logger *slog.Logger, defaults Options) *Hub {
	if defaults.MaxPlayers <= 0 {
		defaults.MaxPlayers = 8
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		logger:   logger,
		defaults: defaults,
		rooms:    make(map[string]*Room),
	}
}

func (h *Hub) Create(hostNickname string) *Room {
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
	opts.Config.CreatorNick = hostNickname
	opts.Logger = h.logger

	r := Start(opts)
	h.rooms[code] = r
	h.logger.Info("room created", "room_id", r.Snapshot().ID, "join_code", code)

	// Inactivity watchdog: if no player joins within TTL, automatically close the abandoned room
	inactivityTimer := time.AfterFunc(lobbyInactivityTTL, func() {
		if r.Snapshot().PlayerCount == 0 && r.Snapshot().State == PhaseLobby {
			h.logger.Info("room expired due to inactivity", "join_code", code)
			r.Close()
		}
	})

	go func() {
		<-r.Done()
		inactivityTimer.Stop()
		h.mu.Lock()
		delete(h.rooms, code)
		h.mu.Unlock()
		h.logger.Info("room removed", "join_code", code)
	}()

	return r
}

func (h *Hub) Get(code string) (*Room, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r, ok := h.rooms[strings.ToUpper(strings.TrimSpace(code))]
	return r, ok
}

func (h *Hub) Shutdown(wait time.Duration) int {
	h.mu.RLock()
	rooms := make([]*Room, 0, len(h.rooms))
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
	return randHex(idBytes)
}
