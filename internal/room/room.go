package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"scope/internal/game"
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
	ID              string
	JoinCode        string
	CreatorNickname string
	MaxPlayers      int
	Logger          *slog.Logger
	Picker          func(n int) []game.Location
	Config          game.Config
	Store           Store
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
	id              string
	joinCode        string
	creatorNickname string
	logger          *slog.Logger

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

// Start launches a new room actor loop running in a dedicated goroutine.
func Start(opts Options) *Room {
	if opts.MaxPlayers <= 0 {
		opts.MaxPlayers = 8
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.Config.MaxPlayers = opts.MaxPlayers

	r := &Room{
		id:              opts.ID,
		joinCode:        opts.JoinCode,
		creatorNickname: opts.CreatorNickname,
		logger:          opts.Logger,
		engine:          game.New(opts.Config, opts.Picker),
		sessions:        make(map[game.PlayerID]*Session),
		timers:          make(map[game.TimerKind]*time.Timer),
		store:           opts.Store,
		commands:        make(chan command, commandBuffer),
		done:            make(chan struct{}),
	}
	r.publish()
	go r.run()

	return r
}

// Attach connects an incoming WebSocket session to the room and registers the player.
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

// NotifyInbound enqueues an incoming client message to the room actor queue.
func (r *Room) NotifyInbound(playerID string, env Envelope) {
	r.deliver(inboundCommand{playerID: game.PlayerID(playerID), env: env})
}

// NotifyDisconnect enqueues a player disconnection notice to the room actor queue.
func (r *Room) NotifyDisconnect(playerID string) {
	r.deliver(disconnectCommand{playerID: game.PlayerID(playerID)})
}

// Snapshot returns an atomic point-in-time copy of current room metadata.
func (r *Room) Snapshot() Snapshot {
	return *r.snapshot.Load()
}

// Done returns a channel that is closed when the room has completely shut down.
func (r *Room) Done() <-chan struct{} {
	return r.done
}

// Close initiates graceful closure of the room command pipeline.
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

// run executes the single-threaded actor loop processing room commands sequentially.
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

// shutdown cleans up timers, disconnects sessions, and waits for background operations.
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

// handleAttach processes player registration and notifies the game engine.
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

// handleInbound decodes incoming client payloads and applies them to the game engine.
func (r *Room) handleInbound(cmd inboundCommand) {
	session, ok := r.sessions[cmd.playerID]
	if !ok {
		return
	}

	switch cmd.env.Type {
	case TypeGuess:
		var guess struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		}
		if json.Unmarshal(cmd.env.Payload, &guess) != nil {
			sendError(session, "invalid guess payload")
			return
		}
		r.execute(r.engine.Apply(game.GuessEvent{
			PlayerID: cmd.playerID,
			Guess:    game.LatLng{Lat: guess.Lat, Lng: guess.Lng},
		}))

	case TypeStartGame:
		r.execute(r.engine.Apply(game.StartEvent{PlayerID: cmd.playerID}))

	default:
		sendError(session, "unsupported message type")
	}
}

// handleDisconnect removes a disconnected player and updates the game engine.
func (r *Room) handleDisconnect(cmd disconnectCommand) {
	if _, ok := r.sessions[cmd.playerID]; !ok {
		return
	}

	delete(r.sessions, cmd.playerID)
	r.execute(r.engine.Apply(game.LeaveEvent{PlayerID: cmd.playerID}))
}

// execute interprets game engine actions, updating clients, timers, and persistence.
func (r *Room) execute(acts []game.Action) {
	for _, a := range acts {
		switch act := a.(type) {
		case game.PlayerJoinedAction:
			r.sendTo(act.PlayerID, joinedEnvelope(act.PlayerID, act.Host))

		case game.RejectedAction:
			if session, ok := r.sessions[act.PlayerID]; ok {
				sendError(session, act.Reason)
			}

		case game.RosterChangedAction:
			r.broadcastRoster()

		case game.GameStartedAction:
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
			env, _ := NewEnvelope(TypeGuessAck, map[string]int{"round": act.Round})
			r.sendTo(act.PlayerID, env)

		case game.RoundRevealedAction:
			// Cancel pending round deadline timer so it doesn't fire spurious timeout commands
			r.stopTimer(game.TimerDeadline)
			env, _ := NewEnvelope(TypeRoundResult, roundResultPayload{
				Round:   act.Round,
				Target:  act.Target,
				Results: act.Results,
			})
			r.broadcast(env)

		case game.GameEndedAction:
			gameID := newGameID()
			env, _ := NewEnvelope(TypeGameOver, gameOverPayload{
				GameID:    gameID,
				Standings: act.Standings,
			})
			r.broadcast(env)
			r.saveGame(gameID, act.Standings, act.Rounds)

		case game.TimerScheduledAction:
			r.armTimer(act.Tag, act.Delay)

		case game.RoomEmptyAction:
			r.Close()
		}
	}

	r.publish()
}

// armTimer schedules a callback timer to post a timeoutCommand to the room actor queue.
func (r *Room) armTimer(tag game.TimerTag, delay time.Duration) {
	r.stopTimer(tag.Kind)
	r.timers[tag.Kind] = time.AfterFunc(delay, func() {
		r.deliver(timeoutCommand{tag: tag})
	})
}

// stopTimer cancels and removes an active timer of the specified kind.
func (r *Room) stopTimer(kind game.TimerKind) {
	if old, exists := r.timers[kind]; exists && old != nil {
		old.Stop()
		delete(r.timers, kind)
	}
}

// stopTimers cancels and clears all active timers for the room.
func (r *Room) stopTimers() {
	for kind, t := range r.timers {
		t.Stop()
		delete(r.timers, kind)
	}
}

// saveGame asynchronously persists finished match standings and round history to storage.
func (r *Room) saveGame(gameID string, standings []game.Standing, rounds []game.FinishedRound) {
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

// publishClosed updates the atomic snapshot to reflect that the room is closed.
func (r *Room) publishClosed() {
	hostNick := r.creatorNickname
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

// publish synchronizes the atomic snapshot with current engine state and player counts.
func (r *Room) publish() {
	state := Phase(r.engine.Phase())
	hostNick := r.creatorNickname
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

// broadcastRoster sends the updated player roster to all connected sessions.
func (r *Room) broadcastRoster() {
	env, _ := NewEnvelope(TypeRoster, rosterPayload{Players: r.engine.Roster()})
	r.broadcast(env)
}

// sendTo delivers an envelope message to a specific player session.
func (r *Room) sendTo(playerID game.PlayerID, env Envelope) {
	if session, ok := r.sessions[playerID]; ok {
		if !session.Send(env) {
			session.Kick()
		}
	}
}

// broadcast sends an envelope message to all active sessions in the room.
func (r *Room) broadcast(env Envelope) {
	for _, session := range r.sessions {
		if !session.Send(env) {
			session.Kick()
		}
	}
}

// deliver safely queues a command to the room's command channel.
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

// joinedEnvelope constructs an envelope confirming successful entry to a room.
func joinedEnvelope(id game.PlayerID, host bool) Envelope {
	env, _ := NewEnvelope(TypeJoined, joinedPayload{
		PlayerID: string(id),
		Host:     host,
	})
	return env
}

// sendError transmits an error envelope to a session, terminating it if delivery fails.
func sendError(session *Session, msg string) {
	env, _ := NewEnvelope(TypeError, map[string]string{"error": msg})
	if !session.Send(env) {
		session.Kick()
	}
}

// findRejected inspects engine actions for a rejection affecting the specified player.
func findRejected(acts []game.Action, playerID game.PlayerID) (string, bool) {
	for _, a := range acts {
		if rej, ok := a.(game.RejectedAction); ok && rej.PlayerID == playerID {
			return rej.Reason, true
		}
	}
	return "", false
}

// newPlayerID generates a unique identifier for a player.
func newPlayerID() game.PlayerID {
	return game.PlayerID(randHex(8))
}

// newGameID generates a unique identifier for a finished game record.
func newGameID() string {
	return randHex(12)
}

// randHex generates a cryptographically secure random hexadecimal string.
func randHex(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 8
	}
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
