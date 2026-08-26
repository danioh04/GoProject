package wsutil

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
	SendBuffer      = 16
	PingInterval    = 25 * time.Second
	PongTimeout     = 90 * time.Second
	WriteTimeout    = 5 * time.Second

	TypeJoin        = "join"
	TypeGuess       = "guess"
	TypeLeave       = "leave"
	TypeStartGame   = "start_game"
	TypePing        = "ping"
	TypeJoined      = "joined"
	TypeRoster      = "roster"
	TypeGameStart   = "game_start"
	TypeRoundStart  = "round_start"
	TypeGuessAck    = "guess_ack"
	TypeRoundResult = "round_result"
	TypeGameOver    = "game_over"
	TypeKicked      = "kicked"
	TypePong        = "pong"
	TypeError       = "error"
)

type Envelope struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	Token   string          `json:"token,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

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

func WriteEnvelope(ctx context.Context, conn Conn, env Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	wctx, cancel := context.WithTimeout(ctx, WriteTimeout)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func WriteError(ctx context.Context, conn Conn, msg string) {
	env, _ := NewEnvelope(TypeError, map[string]string{"error": msg})
	if err := WriteEnvelope(ctx, conn, env); err != nil {
		conn.CloseNow()
		return
	}
	conn.Close(websocket.StatusPolicyViolation, msg)
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

func (s *Session) Send(env Envelope) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.send <- env:
		return true
	default:
		return false
	}
}

func (s *Session) Done() <-chan struct{} {
	return s.done
}

func (s *Session) Kick() {
	s.teardown()
}

func (s *Session) teardown() {
	s.teardownOnce.Do(func() {
		close(s.done)
		s.conn.CloseNow()
	})
}

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

func (s *Session) readPump(onMessage func(Envelope)) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), PongTimeout)
		_, data, err := s.conn.Read(ctx)
		cancel()
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
			s.conn.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}
