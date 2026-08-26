package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"geoduel/internal/hub"
	"geoduel/internal/store"
	"geoduel/internal/wsutil"
	"geoduel/web"
)

const maxNicknameLen = 24

type Persistence interface {
	HardestLocations(ctx context.Context, limit int) ([]store.LocationStat, error)
	GameDetail(ctx context.Context, id string) (*store.GameDetail, error)
}

func New(logger *slog.Logger, rooms *hub.Hub, persistence Persistence) http.Handler {
	s := &server{logger: logger, rooms: rooms, stats: persistence}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/rooms", s.handleCreateRoom)
	mux.HandleFunc("GET /v1/rooms/{code}", s.handleGetRoom)
	mux.HandleFunc("GET /v1/ws", s.handleWS)
	mux.HandleFunc("GET /v1/stats/hardest", s.handleHardestLocations)
	mux.HandleFunc("GET /v1/games/{id}", s.handleGameDetail)
	mux.Handle("GET /", http.FileServerFS(web.Static()))

	return logRequests(logger)(recoverPanics(logger)(mux))
}

type server struct {
	logger *slog.Logger
	rooms  *hub.Hub
	stats  Persistence
}

func (s *server) handleHardestLocations(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeErr(w, http.StatusServiceUnavailable, "persistence disabled")
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			writeErr(w, http.StatusBadRequest, "limit must be between 1 and 50")
			return
		}
		limit = parsed
	}
	stats, err := s.stats.HardestLocations(r.Context(), limit)
	if err != nil {
		s.logger.Error("hardest locations query failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "stats unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": stats})
}

func (s *server) handleGameDetail(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeErr(w, http.StatusServiceUnavailable, "persistence disabled")
		return
	}
	detail, err := s.stats.GameDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		s.logger.Error("game detail query failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "game lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createRoomRequest struct {
	Nickname string `json:"nickname"`
}

func (s *server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomRequest
	if err := decodeStrict(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	nickname, ok := normalizeNickname(req.Nickname)
	if !ok {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("nickname must be 1-%d characters", maxNicknameLen))
		return
	}

	room := s.rooms.Create(nickname)
	writeJSON(w, http.StatusCreated, room.Snapshot())
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")

	nickname, ok := normalizeNickname(r.URL.Query().Get("name"))
	if !ok {
		s.logger.Info("ws handshake rejected", "reason", "invalid name", "remote", r.RemoteAddr)
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("name must be 1-%d characters", maxNicknameLen))
		return
	}
	if !hub.ValidJoinCode(code) {
		s.logger.Info("ws handshake rejected", "reason", "invalid code", "code", code)
		writeErr(w, http.StatusBadRequest, "invalid join code")
		return
	}
	room, exists := s.rooms.Get(code)
	if !exists {
		s.logger.Info("ws handshake rejected", "reason", "room not found", "code", code)
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}

	conn, err := wsutil.Accept(w, r)
	if err != nil {
		s.logger.Error("websocket accept failed", "error", err)
		return
	}

	session := wsutil.NewSession(conn, wsutil.SessionConfig{})
	outcome := room.Attach(session, nickname)
	if !outcome.Accepted {
		wsutil.WriteError(r.Context(), conn, outcome.Reason)
		s.logger.Info("attach rejected", "join_code", room.Snapshot().JoinCode, "reason", outcome.Reason)
		return
	}

	playerID := outcome.PlayerID
	session.Run(func(env wsutil.Envelope) {
		room.NotifyInbound(playerID, env)
	}, func() {
		room.NotifyDisconnect(playerID)
	})
}

func (s *server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !hub.ValidJoinCode(code) {
		writeErr(w, http.StatusBadRequest, "invalid join code")
		return
	}

	room, ok := s.rooms.Get(code)
	if !ok {
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}
	writeJSON(w, http.StatusOK, room.Snapshot())
}

func normalizeNickname(raw string) (string, bool) {
	n := strings.TrimSpace(raw)
	if n == "" || len([]rune(n)) > maxNicknameLen {
		return "", false
	}
	return n, true
}

func decodeStrict(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	w.status = http.StatusSwitchingProtocols
	return h.Hijack()
}

func logRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func recoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						"error", rec,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
