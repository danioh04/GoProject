package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"prism/internal/game"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	commandBuffer = 128
)

type Phase string

const (
	PhaseLobby    Phase = "lobby"
	PhasePlaying  Phase = "playing"
	PhaseReveal   Phase = "reveal"
	PhaseFinished Phase = "finished"
	PhaseClosed   Phase = "closed"
)

type Snapshot struct {
	ID           string `json:"room_id"`
	JoinCode     string `json:"join_code"`
	State        Phase  `json:"state"`
	PlayerCount  int    `json:"player_count"`
	HostNickname string `json:"host_nickname"`
}

type Options struct {
	ID         string
	JoinCode   string
	Label      string
	MaxPlayers int
	Logger     *slog.Logger
	Picker     func(n int) []game.Location
	Config     game.Config
	Store      Store
}

type FinishedGame struct {
	ID          string
	CreatedAt   time.Time
	TotalRounds int
	Standings   []game.Standing
	Rounds      []game.FinishedRound
}

type Store interface {
	SaveGame(ctx context.Context, g FinishedGame) error
}

type AttachOutcome struct {
	Accepted bool
	Reason   string
	PlayerID string
}

type Room struct {
	id       string
	joinCode string
	label    string
	logger   *slog.Logger

	engine   *game.Engine
	sessions map[game.PlayerID]*Session
	timers   map[game.TimerKind]*time.Timer
	store    Store

	commands chan command
	done     chan struct{}
	snapshot atomic.Pointer[Snapshot]

	closeMu sync.RWMutex
	closed  bool
	bgWg    sync.WaitGroup
}

func Start(opts Options) *Room {
	if opts.MaxPlayers <= 0 {
		opts.MaxPlayers = 8
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.Config.MaxPlayers = opts.MaxPlayers

	r := &Room{
		id:       opts.ID,
		joinCode: opts.JoinCode,
		label:    opts.Label,
		logger:   opts.Logger,
		engine:   game.New(opts.Config, opts.Picker),
		sessions: make(map[game.PlayerID]*Session),
		timers:   make(map[game.TimerKind]*time.Timer),
		store:    opts.Store,
		commands: make(chan command, commandBuffer),
		done:     make(chan struct{}),
	}
	r.publish()
	go r.run()
	return r
}

func (r *Room) Attach(session *Session, nickname string) AttachOutcome {
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

func (r *Room) NotifyInbound(playerID string, env Envelope) {
	r.deliver(inboundCommand{playerID: game.PlayerID(playerID), env: env})
}

func (r *Room) NotifyDisconnect(playerID string) {
	r.deliver(disconnectCommand{playerID: game.PlayerID(playerID)})
}

func (r *Room) Snapshot() Snapshot {
	return *r.snapshot.Load()
}

func (r *Room) Done() <-chan struct{} {
	return r.done
}

func (r *Room) Close() {
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	close(r.commands)
}

type command any

type attachCommand struct {
	session  *Session
	nickname string
	reply    chan AttachOutcome
}

type inboundCommand struct {
	playerID game.PlayerID
	env      Envelope
}

type disconnectCommand struct {
	playerID game.PlayerID
}

type timeoutCommand struct {
	tag game.TimerTag
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
		case timeoutCommand:
			r.execute(r.engine.Apply(game.TimeoutEvent{Tag: c.tag}))
		}
	}
}

func (r *Room) shutdown() {
	r.stopTimers()
	kicked, _ := NewEnvelope(TypeKicked, map[string]string{"reason": "room closed"})
	for _, session := range r.sessions {
		session.Send(kicked)
		session.Kick()
	}
	r.publishClosed()
	r.bgWg.Wait()
	close(r.done)
}

func (r *Room) handleAttach(cmd attachCommand) {
	playerID := newPlayerID()
	acts := r.engine.Apply(game.JoinEvent{PlayerID: playerID, Nickname: cmd.nickname})
	if reason, rejected := findRejected(acts, playerID); rejected {
		cmd.reply <- AttachOutcome{Reason: reason}
		return
	}

	r.sessions[playerID] = cmd.session
	cmd.reply <- AttachOutcome{Accepted: true, PlayerID: string(playerID)}
	r.execute(acts)
}

func (r *Room) handleInbound(cmd inboundCommand) {
	session, ok := r.sessions[cmd.playerID]
	if !ok {
		return
	}

	switch cmd.env.Type {
	case TypeGuess:
		var p struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		}
		if json.Unmarshal(cmd.env.Payload, &p) != nil {
			sendError(session, "invalid guess payload")
			return
		}
		r.execute(r.engine.Apply(game.GuessEvent{
			PlayerID: cmd.playerID,
			Guess:    game.LatLng{Lat: p.Lat, Lng: p.Lng},
		}))

	case TypeStartGame:
		r.execute(r.engine.Apply(game.StartEvent{PlayerID: cmd.playerID}))

	default:
		sendError(session, "unsupported message type")
	}
}

func (r *Room) handleDisconnect(cmd disconnectCommand) {
	if _, ok := r.sessions[cmd.playerID]; !ok {
		return
	}
	delete(r.sessions, cmd.playerID)
	r.execute(r.engine.Apply(game.LeaveEvent{PlayerID: cmd.playerID}))
}

func (r *Room) execute(acts []game.Action) {
	for _, a := range acts {
		switch act := a.(type) {
		case game.PlayerJoinedAction:
			if session, ok := r.sessions[act.PlayerID]; ok {
				env := joinedEnvelope(act.PlayerID, act.Host)
				if !session.Send(env) {
					session.Kick()
				}
			}

		case game.RejectedAction:
			if session, ok := r.sessions[act.PlayerID]; ok {
				sendError(session, act.Reason)
			}

		case game.RosterChangedAction:
			r.broadcastRoster()

		case game.MatchStartedAction:
			env, _ := NewEnvelope(TypeGameStart, map[string]int{"total_rounds": act.TotalRounds})
			r.broadcast(env)

		case game.RoundStartedAction:
			env, _ := NewEnvelope(TypeRoundStart, roundStartPayload{
				Round:        act.Round,
				TotalRounds:  act.TotalRounds,
				PanoID:       act.PanoID,
				DeadlineUnix: act.Deadline.Unix(),
				Seconds:      act.RoundSeconds,
			})
			r.broadcast(env)

		case game.GuessAcceptedAction:
			if session, ok := r.sessions[act.PlayerID]; ok {
				env, _ := NewEnvelope(TypeGuessAck, map[string]int{"round": act.Round})
				if !session.Send(env) {
					session.Kick()
				}
			}

		case game.RoundRevealedAction:
			// Cancel pending round deadline timer so it doesn't fire spurious timeout commands
			r.stopTimer(game.TimerDeadline)
			env, _ := NewEnvelope(TypeRoundResult, roundResultPayload{
				Round:   act.Round,
				Target:  act.Target,
				Results: act.Results,
			})
			r.broadcast(env)

		case game.MatchEndedAction:
			gameID := newMatchID()
			env, _ := NewEnvelope(TypeGameOver, gameOverPayload{
				GameID:    gameID,
				Standings: act.Standings,
			})
			r.broadcast(env)
			r.flushMatch(gameID, act.Standings, act.Rounds)

		case game.TimerScheduledAction:
			r.armTimer(act.Tag, act.Delay)

		case game.RoomEmptyAction:
			r.Close()
		}
	}
	r.publish()
}

func (r *Room) armTimer(tag game.TimerTag, delay time.Duration) {
	r.stopTimer(tag.Kind)
	r.timers[tag.Kind] = time.AfterFunc(delay, func() {
		r.deliver(timeoutCommand{tag: tag})
	})
}

func (r *Room) stopTimer(kind game.TimerKind) {
	if old, exists := r.timers[kind]; exists && old != nil {
		old.Stop()
		delete(r.timers, kind)
	}
}

func (r *Room) stopTimers() {
	for kind, t := range r.timers {
		t.Stop()
		delete(r.timers, kind)
	}
}

func (r *Room) flushMatch(gameID string, standings []game.Standing, rounds []game.FinishedRound) {
	if r.store == nil {
		return
	}
	fg := FinishedGame{
		ID:          gameID,
		CreatedAt:   time.Now().UTC(),
		TotalRounds: len(rounds),
		Standings:   standings,
		Rounds:      rounds,
	}

	r.bgWg.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.store.SaveGame(ctx, fg); err != nil {
			r.logger.Error("save game failed", "game_id", fg.ID, "error", err)
			return
		}
		r.logger.Info("game saved", "game_id", fg.ID)
	})
}

func (r *Room) publishClosed() {
	hostNick := r.label
	if prev := r.snapshot.Load(); prev != nil && prev.HostNickname != "" {
		hostNick = prev.HostNickname
	}
	r.snapshot.Store(&Snapshot{
		ID:           r.id,
		JoinCode:     r.joinCode,
		State:        PhaseClosed,
		PlayerCount:  0,
		HostNickname: hostNick,
	})
}

func (r *Room) publish() {
	state := PhaseClosed
	switch r.engine.Phase() {
	case game.PhaseLobby:
		state = PhaseLobby
	case game.PhasePlaying:
		state = PhasePlaying
	case game.PhaseReveal:
		state = PhaseReveal
	case game.PhaseFinished:
		state = PhaseFinished
	}
	hostNick := r.label
	for _, p := range r.engine.Roster() {
		if p.IsHost {
			hostNick = p.Nickname
			break
		}
	}
	r.snapshot.Store(&Snapshot{
		ID:           r.id,
		JoinCode:     r.joinCode,
		State:        state,
		PlayerCount:  r.engine.PlayerCount(),
		HostNickname: hostNick,
	})
}

func (r *Room) broadcastRoster() {
	env, _ := NewEnvelope(TypeRoster, rosterPayload{Players: r.engine.Roster()})
	r.broadcast(env)
}

func (r *Room) broadcast(env Envelope) {
	for _, session := range r.sessions {
		if !session.Send(env) {
			session.Kick()
		}
	}
}

// deliver sends a command to the room's inbox while holding closeMu.RLock to prevent send-on-closed-channel.
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

type joinedPayload struct {
	PlayerID string `json:"player_id"`
	Host     bool   `json:"host"`
}

type rosterPayload struct {
	Players []game.Player `json:"players"`
}

type roundStartPayload struct {
	Round        int    `json:"round"`
	TotalRounds  int    `json:"total_rounds"`
	PanoID       string `json:"pano_id"`
	DeadlineUnix int64  `json:"deadline_unix"`
	Seconds      int    `json:"seconds"`
}

type roundResultPayload struct {
	Round   int                `json:"round"`
	Target  game.LatLng        `json:"target"`
	Results []game.RoundResult `json:"results"`
}

type gameOverPayload struct {
	GameID    string          `json:"game_id"`
	Standings []game.Standing `json:"standings"`
}

func joinedEnvelope(id game.PlayerID, host bool) Envelope {
	env, _ := NewEnvelope(TypeJoined, joinedPayload{
		PlayerID: string(id),
		Host:     host,
	})
	return env
}

func sendError(session *Session, msg string) {
	env, _ := NewEnvelope(TypeError, map[string]string{"error": msg})
	if !session.Send(env) {
		session.Kick()
	}
}

func findRejected(acts []game.Action, playerID game.PlayerID) (string, bool) {
	for _, a := range acts {
		if rej, ok := a.(game.RejectedAction); ok && rej.PlayerID == playerID {
			return rej.Reason, true
		}
	}
	return "", false
}

func newPlayerID() game.PlayerID {
	return game.PlayerID(randHex(8))
}

func newMatchID() string {
	return randHex(12)
}

func randHex(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 8
	}
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
