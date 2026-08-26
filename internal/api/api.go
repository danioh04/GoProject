package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"geoduel/internal/hub"
)

const maxNicknameLen = 24

func New(logger *slog.Logger, rooms *hub.Hub) http.Handler {
	s := &server{logger: logger, rooms: rooms}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/rooms", s.handleCreateRoom)
	mux.HandleFunc("GET /v1/rooms/{code}", s.handleGetRoom)

	return logRequests(logger)(recoverPanics(logger)(mux))
}

type server struct {
	logger *slog.Logger
	rooms  *hub.Hub
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

	nickname := strings.TrimSpace(req.Nickname)
	if nickname == "" || len([]rune(nickname)) > maxNicknameLen {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("nickname must be 1-%d characters", maxNicknameLen))
		return
	}

	room := s.rooms.Create(nickname)
	writeJSON(w, http.StatusCreated, room.Snapshot())
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
