package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	ProtocolVersion = 1
	MaxMessageBytes = 1 << 12
	SendBuffer      = 32
	PingInterval    = 25 * time.Second
	WriteTimeout    = 5 * time.Second

	TypeGuess       = "guess"
	TypeStartGame   = "start_game"
	TypeJoined      = "joined"
	TypeRoster      = "roster"
	TypeGameStart   = "game_start"
	TypeRoundStart  = "round_start"
	TypeGuessAck    = "guess_ack"
	TypeRoundResult = "round_result"
	TypeGameOver    = "game_over"
	TypeKicked      = "kicked"
	TypeError       = "error"
)

type Envelope struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope constructs an envelope with the default protocol version and JSON payload.
func NewEnvelope(typ string, payload any) (Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, fmt.Errorf("marshal payload: %w", err)
		}
		raw = b
	}

	return Envelope{Version: ProtocolVersion, Type: typ, Payload: raw}, nil
}

// DecodeEnvelope parses raw JSON data into an envelope, verifying protocol compatibility.
func DecodeEnvelope(data []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return env, fmt.Errorf("invalid envelope: %w", err)
	}

	if env.Version != ProtocolVersion {
		return env, fmt.Errorf("unsupported protocol version %d", env.Version)
	}
	if env.Type == "" {
		return env, errors.New("missing message type")
	}

	return env, nil
}

type Conn = *websocket.Conn

// Accept upgrades an incoming HTTP request into a WebSocket connection.
func Accept(w http.ResponseWriter, r *http.Request) (Conn, error) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return nil, err
	}

	c.SetReadLimit(MaxMessageBytes)
	return c, nil
}

// WriteEnvelope serializes and writes an envelope to a WebSocket connection with timeout.
func WriteEnvelope(ctx context.Context, conn Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	wctx, cancel := context.WithTimeout(ctx, WriteTimeout)
	defer cancel()

	return conn.Write(wctx, websocket.MessageText, b)
}

// WriteError sends a fatal protocol error envelope and terminates the WebSocket connection.
func WriteError(ctx context.Context, conn Conn, msg string) {
	env, _ := NewEnvelope(TypeError, map[string]string{"error": msg})
	if err := WriteEnvelope(ctx, conn, env); err != nil {
		_ = conn.CloseNow()
		return
	}

	_ = conn.Close(websocket.StatusPolicyViolation, msg)
}

type SessionConfig struct {
	SendBuffer   int
	PingInterval time.Duration
	WriteTimeout time.Duration
}

type Session struct {
	conn         Conn
	cfg          SessionConfig
	send         chan Envelope
	done         chan struct{}
	teardownOnce sync.Once
}

// NewSession initializes a session wrapping a WebSocket connection and channel queues.
func NewSession(conn Conn, cfg SessionConfig) *Session {
	if cfg.SendBuffer <= 0 {
		cfg.SendBuffer = SendBuffer
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = PingInterval
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = WriteTimeout
	}

	return &Session{
		conn: conn,
		cfg:  cfg,
		send: make(chan Envelope, cfg.SendBuffer),
		done: make(chan struct{}),
	}
}

// Run executes concurrent read and write message pumps until disconnection.
func (s *Session) Run(onMessage func(Envelope), onClose func()) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.readPump(onMessage)
	}()

	s.writePump()
	wg.Wait()

	s.teardown()
	if onClose != nil {
		onClose()
	}
}

// Send queues an envelope for transmission over the write pump without blocking.
func (s *Session) Send(env Envelope) bool {
	select {
	case <-s.done:
		return false
	case s.send <- env:
		return true
	default:
		return false
	}
}

// Kick terminates the session and closes the connection.
func (s *Session) Kick() {
	s.teardown()
}

// readPump reads incoming WebSocket messages and invokes the message callback.
func (s *Session) readPump(onMessage func(Envelope)) {
	for {
		_, data, err := s.conn.Read(context.Background())
		if err != nil {
			s.teardown()
			return
		}

		env, derr := DecodeEnvelope(data)
		if derr != nil {
			if rej, _ := NewEnvelope(TypeError, map[string]string{"error": derr.Error()}); !s.Send(rej) {
				s.teardown()
				return
			}
			continue
		}

		if onMessage != nil {
			onMessage(env)
		}
	}
}

// writePump handles outgoing message delivery and sends periodic heartbeat pings.
func (s *Session) writePump() {
	ticker := time.NewTicker(s.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case env := <-s.send:
			if err := WriteEnvelope(context.Background(), s.conn, env); err != nil {
				s.teardown()
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(context.Background(), s.cfg.WriteTimeout)
			err := s.conn.Ping(pctx)
			cancel()
			if err != nil {
				s.teardown()
				return
			}
		case <-s.done:
			return
		}
	}
}

// teardown ensures the connection and done channel are closed once.
func (s *Session) teardown() {
	s.teardownOnce.Do(func() {
		close(s.done)
		_ = s.conn.CloseNow()
	})
}
