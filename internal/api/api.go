package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"scope/internal/room"
	"scope/internal/store"
)

const (
	maxNicknameLen      = 24
	maxRequestBodyBytes = 4 * 1024
)

type Persistence interface {
	ListRecentGames(ctx context.Context, limit int) ([]store.GameSummary, error)
	GameDetail(ctx context.Context, id string) (*store.GameDetail, error)
}

type server struct {
	logger        *slog.Logger
	rooms         *room.Hub
	store         Persistence
	hasStreetView bool
}

// New configures routing and middleware for the HTTP API.
func New(logger *slog.Logger, rooms *room.Hub, persistence Persistence, hasStreetView bool) http.Handler {
	s := &server{logger: logger, rooms: rooms, store: persistence, hasStreetView: hasStreetView}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/rooms", s.handleCreateRoom)
	mux.HandleFunc("GET /v1/rooms/{code}", s.handleGetRoom)
	mux.HandleFunc("GET /v1/ws", s.handleWS)
	mux.HandleFunc("GET /v1/games", s.handleListGames)
	mux.HandleFunc("GET /v1/games/{id}", s.handleGameDetail)

	handler := recoverPanics(logger)(mux)
	handler = logRequests(logger)(handler)
	return requestIDs(handler)
}

// handleRoot serves the service metadata, feature availability, and API directory.
func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "scope",
		"status":  "ok",
		"version": "1.0",
		"features": map[string]bool{
			"streetview":  s.hasStreetView,
			"persistence": s.store != nil,
		},
		"endpoints": map[string]string{
			"health":      "GET /healthz",
			"create_room": "POST /v1/rooms",
			"get_room":    "GET /v1/rooms/{code}",
			"websocket":   "GET /v1/ws?code={code}&nickname={nickname}",
			"list_games":  "GET /v1/games",
			"game_detail": "GET /v1/games/{id}",
		},
	})
}

// handleHealth provides a lightweight healthcheck endpoint.
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createRoomRequest struct {
	Nickname string `json:"nickname"`
}

// handleCreateRoom creates a new game room with the specified host player.
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

// handleWS handles incoming WebSocket connections, attaching players to their target rooms.
func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")

	nickname, ok := normalizeNickname(r.URL.Query().Get("nickname"))
	if !ok {
		s.logger.Info("ws handshake rejected", "reason", "invalid nickname", "remote", r.RemoteAddr)
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("nickname must be 1-%d characters", maxNicknameLen))
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

// handleGetRoom returns the current state and member count of a room.
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

// handleListGames returns recently completed games when persistence is enabled.
func (s *server) handleListGames(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "persistence disabled")
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 && parsed <= 50 {
			limit = parsed
		}
	}

	games, err := s.store.ListRecentGames(r.Context(), limit)
	if err != nil {
		s.logger.Error("list games query failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "games lookup failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"games": games})
}

// handleGameDetail returns comprehensive match history and round results for a game ID.
func (s *server) handleGameDetail(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "persistence disabled")
		return
	}

	detail, err := s.store.GameDetail(r.Context(), r.PathValue("id"))
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

// normalizeNickname trims whitespace and validates player nickname constraints.
func normalizeNickname(raw string) (string, bool) {
	n := strings.TrimSpace(raw)
	if n == "" || len([]rune(n)) > maxNicknameLen {
		return "", false
	}

	return n, true
}

// decodeStrict parses a JSON request body and rejects unknown fields or payloads exceeding max size.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}

	return nil
}

// writeJSON writes a JSON-encoded response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes a standardized JSON error message.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type requestIDKey struct{}

// requestIDs is a middleware that assigns a unique request ID to each incoming HTTP request.
func requestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := randHex(8)
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestIDFrom extracts the request ID from the context.
func requestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}

	return ""
}

// logRequests is a middleware that logs method, path, status, and duration for each request.
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

// WriteHeader captures the HTTP status code before writing to the response writer.
func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter for standard library compatibility.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// recoverPanics is a middleware that recovers from unexpected panics and writes a 500 error.
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

// randHex generates a cryptographically secure random hexadecimal string.
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
