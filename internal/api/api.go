package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"prism/internal/room"
	"prism/internal/store"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const maxNicknameLen = 24

type Persistence interface {
	ListRecentGames(ctx context.Context, limit int) ([]store.GameSummary, error)
	GameDetail(ctx context.Context, id string) (*store.GameDetail, error)
}

type server struct {
	logger        *slog.Logger
	rooms         *room.Hub
	stats         Persistence
	hasStreetView bool
}

func New(logger *slog.Logger, rooms *room.Hub, persistence Persistence, hasStreetView bool) http.Handler {
	s := &server{logger: logger, rooms: rooms, stats: persistence, hasStreetView: hasStreetView}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/rooms", s.handleCreateRoom)
	mux.HandleFunc("GET /v1/rooms/{code}", s.handleGetRoom)
	mux.HandleFunc("GET /v1/ws", s.handleWS)
	mux.HandleFunc("GET /v1/games", s.handleListGames)
	mux.HandleFunc("GET /v1/games/{id}", s.handleGameDetail)

	return requestIDs(logRequests(logger)(recoverPanics(logger)(mux)))
}

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "prism",
		"status":  "ok",
		"version": "1.0",
		"features": map[string]bool{
			"streetview":  s.hasStreetView,
			"persistence": s.stats != nil,
		},
		"endpoints": map[string]string{
			"health":      "GET /healthz",
			"create_room": "POST /v1/rooms",
			"get_room":    "GET /v1/rooms/{code}",
			"websocket":   "GET /v1/ws?code={code}&name={name}",
			"list_games":  "GET /v1/games",
			"game_detail": "GET /v1/games/{id}",
		},
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

	createdRoom := s.rooms.Create(nickname)
	writeJSON(w, http.StatusCreated, createdRoom.Snapshot())
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")

	nickname, ok := normalizeNickname(r.URL.Query().Get("name"))
	if !ok {
		s.logger.Info("ws handshake rejected", "reason", "invalid name", "remote", r.RemoteAddr)
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("name must be 1-%d characters", maxNicknameLen))
		return
	}
	if !room.ValidJoinCode(code) {
		s.logger.Info("ws handshake rejected", "reason", "invalid code", "code", code)
		writeErr(w, http.StatusBadRequest, "invalid join code")
		return
	}
	targetRoom, exists := s.rooms.Get(code)
	if !exists {
		s.logger.Info("ws handshake rejected", "reason", "room not found", "code", code)
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}

	conn, err := room.Accept(w, r)
	if err != nil {
		s.logger.Error("websocket accept failed", "error", err)
		return
	}

	session := room.NewSession(conn, room.SessionConfig{})
	outcome := targetRoom.Attach(session, nickname)
	if !outcome.Accepted {
		room.WriteError(r.Context(), conn, outcome.Reason)
		s.logger.Info("attach rejected", "join_code", targetRoom.Snapshot().JoinCode, "reason", outcome.Reason)
		return
	}

	playerID := outcome.PlayerID
	s.logger.Info("websocket attached", "join_code", code, "player_id", playerID, "nickname", nickname)
	session.Run(func(env room.Envelope) {
		targetRoom.NotifyInbound(playerID, env)
	}, func() {
		s.logger.Info("websocket disconnected", "join_code", code, "player_id", playerID)
		targetRoom.NotifyDisconnect(playerID)
	})
}

func (s *server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !room.ValidJoinCode(code) {
		writeErr(w, http.StatusBadRequest, "invalid join code")
		return
	}

	targetRoom, ok := s.rooms.Get(code)
	if !ok {
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}
	writeJSON(w, http.StatusOK, targetRoom.Snapshot())
}

func (s *server) handleListGames(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeErr(w, http.StatusServiceUnavailable, "persistence disabled")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 && parsed <= 50 {
			limit = parsed
		}
	}
	games, err := s.stats.ListRecentGames(r.Context(), limit)
	if err != nil {
		s.logger.Error("list games query failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "games lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": games})
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

type createRoomRequest struct {
	Nickname string `json:"nickname"`
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
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type requestIDKey struct{}

func requestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := randHex(8)
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
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
				"request_id", requestIDFrom(r.Context()),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
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

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
