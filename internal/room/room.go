package room

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"geoduel/internal/wsutil"
)

const (
	idBytes       = 8
	tokenBytes    = 16
	commandBuffer = 128
)

type Phase string

const (
	PhaseLobby  Phase = "lobby"
	PhaseClosed Phase = "closed"
)

type Snapshot struct {
	ID           string `json:"room_id"`
	JoinCode     string `json:"join_code"`
	State        Phase  `json:"state"`
	PlayerCount  int    `json:"player_count"`
	HostNickname string `json:"host_nickname"`
}

type RosterPlayer struct {
	PlayerID string `json:"player_id"`
	Nickname string `json:"nickname"`
	IsHost   bool   `json:"is_host"`
}

type rosterPayload struct {
	Players []RosterPlayer `json:"players"`
}

type AttachOutcome struct {
	Accepted bool
	Reason   string
	PlayerID string
	Token    string
}

type Player struct {
	ID       string
	Token    string
	Nickname string
	Host     bool
	session  *wsutil.Session
}

func (p *Player) authorized(token string) bool {
	return subtle.ConstantTimeCompare([]byte(p.Token), []byte(token)) == 1
}

type command interface{}

type attachCommand struct {
	session  *wsutil.Session
	nickname string
	reply    chan AttachOutcome
}

type inboundCommand struct {
	playerID string
	env      wsutil.Envelope
}

type disconnectCommand struct {
	playerID string
}

type Room struct {
	id         string
	joinCode   string
	label      string
	maxPlayers int
	logger     *slog.Logger

	commands  chan command
	done      chan struct{}
	snapshot  atomic.Pointer[Snapshot]
	closeOnce sync.Once
	closeMu   sync.RWMutex
	closed    bool

	players map[string]*Player
	order   []string
}

func Start(id, joinCode, label string, maxPlayers int, logger *slog.Logger) *Room {
	if maxPlayers <= 0 {
		maxPlayers = 8
	}
	r := &Room{
		id:         id,
		joinCode:   joinCode,
		label:      label,
		maxPlayers: maxPlayers,
		logger:     logger,
		commands:   make(chan command, commandBuffer),
		done:       make(chan struct{}),
		players:    make(map[string]*Player),
	}
	r.publish(PhaseLobby, 0)
	go r.run()
	return r
}

func (r *Room) run() {
	defer r.shutdown()
	for cmd := range r.commands {
		switch c := cmd.(type) {
		case attachCommand:
			r.handleAttach(c)
		case inboundCommand:
			r.handleInbound(c)
		case disconnectCommand:
			r.handleDisconnect(c)
		}
	}
}

func (r *Room) shutdown() {
	r.publish(PhaseClosed, 0)
	kicked, _ := wsutil.NewEnvelope(wsutil.TypeKicked, map[string]string{"reason": "room closed"})
	for _, p := range r.playerList() {
		p.session.Send(kicked)
		p.session.Kick()
	}
	close(r.done)
}

func (r *Room) handleAttach(cmd attachCommand) {
	outcome := func() AttachOutcome {
		if len(r.order) >= r.maxPlayers {
			return AttachOutcome{Reason: "room full"}
		}
		for _, p := range r.players {
			if strings.EqualFold(p.Nickname, cmd.nickname) {
				return AttachOutcome{Reason: "nickname already taken"}
			}
		}
		id, token, err := mintIdentity()
		if err != nil {
			r.logger.Error("mint identity", "error", err)
			return AttachOutcome{Reason: "internal error"}
		}
		player := &Player{
			ID:       id,
			Token:    token,
			Nickname: cmd.nickname,
			Host:     len(r.order) == 0,
			session:  cmd.session,
		}
		r.players[id] = player
		r.order = append(r.order, id)
		return AttachOutcome{Accepted: true, PlayerID: id, Token: token}
	}()

	if !outcome.Accepted {
		cmd.reply <- outcome
		return
	}

	joined, _ := wsutil.NewEnvelope(wsutil.TypeJoined, map[string]any{
		"player_id": outcome.PlayerID,
		"token":     outcome.Token,
		"host":      r.players[outcome.PlayerID].Host,
	})
	r.players[outcome.PlayerID].session.Send(joined)
	r.broadcastRoster()
	r.publish(PhaseLobby, len(r.order))
	r.logger.Info("player attached", "room_id", r.id, "player_id", outcome.PlayerID)
	cmd.reply <- outcome
}

func (r *Room) handleInbound(cmd inboundCommand) {
	p, ok := r.players[cmd.playerID]
	if !ok {
		return
	}
	switch cmd.env.Type {
	case wsutil.TypePing:
		if !p.authorized(cmd.env.Token) {
			r.directError(p, "invalid or missing token")
			return
		}
		pong, _ := wsutil.NewEnvelope(wsutil.TypePong, nil)
		if !p.session.Send(pong) {
			p.session.Kick()
		}
	default:
		r.directError(p, "unsupported message type")
	}
}

func (r *Room) handleDisconnect(cmd disconnectCommand) {
	p, ok := r.players[cmd.playerID]
	if !ok {
		return
	}
	delete(r.players, cmd.playerID)
	for i, id := range r.order {
		if id == cmd.playerID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}

	if p.Host && len(r.order) > 0 {
		r.players[r.order[0]].Host = true
		r.logger.Info("host promoted", "room_id", r.id, "player_id", r.order[0])
	}
	r.publish(PhaseLobby, len(r.order))
	r.logger.Info("player disconnected", "room_id", r.id, "player_id", cmd.playerID)

	if len(r.order) == 0 {
		r.Close()
		return
	}
	r.broadcastRoster()
}

func (r *Room) Attach(session *wsutil.Session, nickname string) AttachOutcome {
	cmd := attachCommand{session: session, nickname: nickname, reply: make(chan AttachOutcome, 1)}
	if !r.deliver(cmd) {
		return AttachOutcome{Reason: "room closed"}
	}
	select {
	case out := <-cmd.reply:
		return out
	case <-r.done:
		return AttachOutcome{Reason: "room closed"}
	}
}

func (r *Room) NotifyInbound(playerID string, env wsutil.Envelope) {
	r.deliver(inboundCommand{playerID: playerID, env: env})
}

func (r *Room) NotifyDisconnect(playerID string) {
	r.deliver(disconnectCommand{playerID: playerID})
}

func (r *Room) Close() {
	r.closeMu.Lock()
	if r.closed {
		r.closeMu.Unlock()
		return
	}
	r.closed = true
	r.closeMu.Unlock()
	close(r.commands)
}

func (r *Room) Done() <-chan struct{} {
	return r.done
}

func (r *Room) Snapshot() Snapshot {
	return *r.snapshot.Load()
}

func (r *Room) deliver(cmd command) bool {
	r.closeMu.RLock()
	defer r.closeMu.RUnlock()
	if r.closed {
		return false
	}
	select {
	case r.commands <- cmd:
		return true
	case <-r.done:
		return false
	}
}

func (r *Room) broadcastRoster() {
	payload := rosterPayload{Players: make([]RosterPlayer, 0, len(r.order))}
	for _, id := range r.order {
		p := r.players[id]
		payload.Players = append(payload.Players, RosterPlayer{
			PlayerID: p.ID,
			Nickname: p.Nickname,
			IsHost:   p.Host,
		})
	}
	env, _ := wsutil.NewEnvelope(wsutil.TypeRoster, payload)
	for _, p := range r.playerList() {
		if !p.session.Send(env) {
			p.session.Kick()
		}
	}
}

func (r *Room) directError(p *Player, msg string) {
	env, _ := wsutil.NewEnvelope(wsutil.TypeError, map[string]string{"error": msg})
	if !p.session.Send(env) {
		p.session.Kick()
	}
}

func (r *Room) playerList() []*Player {
	list := make([]*Player, 0, len(r.order))
	for _, id := range r.order {
		list = append(list, r.players[id])
	}
	return list
}

func (r *Room) publish(state Phase, playerCount int) {
	r.snapshot.Store(&Snapshot{
		ID:           r.id,
		JoinCode:     r.joinCode,
		State:        state,
		PlayerCount:  playerCount,
		HostNickname: r.label,
	})
}

func mintIdentity() (id, token string, err error) {
	var b [idBytes + tokenBytes]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	marshalled := hex.EncodeToString(b[:])
	return marshalled[:idBytes*2], marshalled[idBytes*2:], nil
}
