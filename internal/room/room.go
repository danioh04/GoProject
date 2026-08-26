package room

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"geoduel/internal/game"
	"geoduel/internal/metrics"
	"geoduel/internal/wsutil"
)

const (
	tokenBytes    = 16
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
	Rounds      []FinishedRound
}

type FinishedRound struct {
	Number     int
	LocationID string
	Target     game.LatLng
	Results    []game.RoundResult
}

type Store interface {
	SaveGame(ctx context.Context, g FinishedGame) error
}

type AttachOutcome struct {
	Accepted bool
	Reason   string
	PlayerID string
	Token    string
}

type connMeta struct {
	session *wsutil.Session
	token   string
}

type command interface{}

type attachCommand struct {
	session  *wsutil.Session
	nickname string
	reply    chan AttachOutcome
}

type inboundCommand struct {
	playerID game.PlayerID
	env      wsutil.Envelope
}

type disconnectCommand struct {
	playerID game.PlayerID
}

type timeoutCommand struct {
	tag game.TimerTag
}

type Room struct {
	id       string
	joinCode string
	label    string
	cfg      game.Config
	logger   *slog.Logger

	engine   *game.Engine
	sessions map[game.PlayerID]connMeta
	timers   map[game.TimerKind]*time.Timer
	store    Store
	match    *matchLog

	commands chan command
	done     chan struct{}
	snapshot atomic.Pointer[Snapshot]

	closeOnce sync.Once
	closeMu   sync.RWMutex
	closed    bool
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
		cfg:      opts.Config,
		logger:   opts.Logger,
		engine:   game.New(opts.Config, time.Now, opts.Picker),
		sessions: make(map[game.PlayerID]connMeta),
		timers:   make(map[game.TimerKind]*time.Timer),
		store:    opts.Store,
		commands: make(chan command, commandBuffer),
		done:     make(chan struct{}),
	}
	r.publish()
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
		case timeoutCommand:
			r.execute(r.engine.Apply(game.TimeoutEvent{Tag: c.tag}))
		}
	}
}

func (r *Room) shutdown() {
	r.stopTimers()
	kicked, _ := wsutil.NewEnvelope(wsutil.TypeKicked, map[string]string{"reason": "room closed"})
	for _, meta := range r.sessions {
		meta.session.Send(kicked)
		meta.session.Kick()
	}
	r.publishClosed()
	close(r.done)
}

func (r *Room) handleAttach(cmd attachCommand) {
	playerID, token, err := mintTokenPair()
	if err != nil {
		r.logger.Error("mint identity", "error", err)
		cmd.reply <- AttachOutcome{Reason: "internal error"}
		return
	}

	acts := r.engine.Apply(game.JoinEvent{PlayerID: playerID, Nickname: cmd.nickname})
	if reason, rejected := findRejected(acts, playerID); rejected {
		cmd.reply <- AttachOutcome{Reason: reason}
		return
	}

	r.sessions[playerID] = connMeta{session: cmd.session, token: token}
	cmd.reply <- AttachOutcome{Accepted: true, PlayerID: string(playerID), Token: token}
	r.execute(acts)
}

func (r *Room) handleInbound(cmd inboundCommand) {
	meta, ok := r.sessions[cmd.playerID]
	if !ok {
		return
	}

	switch cmd.env.Type {
	case wsutil.TypePing:
		if !tokenMatches(meta.token, cmd.env.Token) {
			sendError(meta.session, "invalid or missing token")
			return
		}
		pong, _ := wsutil.NewEnvelope(wsutil.TypePong, nil)
		if !meta.session.Send(pong) {
			meta.session.Kick()
		}

	case wsutil.TypeGuess:
		if !tokenMatches(meta.token, cmd.env.Token) {
			sendError(meta.session, "invalid or missing token")
			return
		}
		var p struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		}
		if json.Unmarshal(cmd.env.Payload, &p) != nil {
			sendError(meta.session, "invalid guess payload")
			return
		}
		r.execute(r.engine.Apply(game.GuessEvent{
			PlayerID: cmd.playerID,
			Guess:    game.LatLng{Lat: p.Lat, Lng: p.Lng},
		}))

	case wsutil.TypeStartGame:
		if !tokenMatches(meta.token, cmd.env.Token) {
			sendError(meta.session, "invalid or missing token")
			return
		}
		r.execute(r.engine.Apply(game.StartEvent{PlayerID: cmd.playerID}))

	default:
		sendError(meta.session, "unsupported message type")
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
			meta := r.sessions[act.PlayerID]
			env := joinedEnvelope(act.PlayerID, meta.token, act.Host)
			if !meta.session.Send(env) {
				meta.session.Kick()
			}

		case game.RejectedAction:
			if meta, ok := r.sessions[act.PlayerID]; ok {
				sendError(meta.session, act.Reason)
			}

		case game.RosterChangedAction:
			r.broadcastRoster()

		case game.MatchStartedAction:
			env, _ := wsutil.NewEnvelope(wsutil.TypeGameStart, map[string]int{"total_rounds": act.TotalRounds})
			r.broadcast(env)
			r.match = &matchLog{
				createdAt: time.Now().UTC(),
				rounds:    make([]FinishedRound, act.TotalRounds),
			}

		case game.RoundStartedAction:
			r.logRoundLocation(act.Round, act.Location.ID)
			env, _ := wsutil.NewEnvelope(wsutil.TypeRoundStart, roundStartPayload{
				Round:        act.Round,
				TotalRounds:  act.TotalRounds,
				PanoID:       act.Location.PanoID,
				Hint:         act.Hint,
				DeadlineUnix: act.Deadline.Unix(),
				Seconds:      act.RoundSeconds,
			})
			r.broadcast(env)

		case game.GuessAcceptedAction:
			metrics.GuessesTotal.Add(1)
			if meta, ok := r.sessions[act.PlayerID]; ok {
				env, _ := wsutil.NewEnvelope(wsutil.TypeGuessAck, map[string]int{"round": act.Round})
				if !meta.session.Send(env) {
					meta.session.Kick()
				}
			}

		case game.RoundRevealedAction:
			r.logReveal(act.Round, act.Target, act.Results)
			env, _ := wsutil.NewEnvelope(wsutil.TypeRoundResult, roundResultPayload{
				Round:   act.Round,
				Target:  act.Target,
				Results: act.Results,
			})
			r.broadcast(env)

		case game.MatchEndedAction:
			env, _ := wsutil.NewEnvelope(wsutil.TypeGameOver, gameOverPayload{Standings: act.Standings})
			r.broadcast(env)
			r.flushMatch(act.Standings)

		case game.TimerScheduledAction:
			r.armTimer(act.Tag, act.Delay)

		case game.RoomEmptyAction:
			r.Close()
		}
	}
	r.publish()
}

func (r *Room) armTimer(tag game.TimerTag, delay time.Duration) {
	if old := r.timers[tag.Kind]; old != nil {
		old.Stop()
	}
	r.timers[tag.Kind] = time.AfterFunc(delay, func() {
		r.deliver(timeoutCommand{tag: tag})
	})
}

type matchLog struct {
	createdAt time.Time
	rounds    []FinishedRound
}

func (r *Room) logRoundLocation(round int, locationID string) {
	if r.match == nil || round < 1 || round > len(r.match.rounds) {
		return
	}
	r.match.rounds[round-1].Number = round
	r.match.rounds[round-1].LocationID = locationID
}

func (r *Room) logReveal(round int, target game.LatLng, results []game.RoundResult) {
	if r.match == nil || round < 1 || round > len(r.match.rounds) {
		return
	}
	dst := &r.match.rounds[round-1]
	dst.Number = round
	dst.Target = target
	dst.Results = make([]game.RoundResult, len(results))
	copy(dst.Results, results)
}

func (r *Room) flushMatch(standings []game.Standing) {
	if r.store == nil || r.match == nil {
		return
	}
	fg := FinishedGame{
		ID:          newMatchID(),
		CreatedAt:   r.match.createdAt,
		TotalRounds: len(r.match.rounds),
		Standings:   standings,
		Rounds:      r.match.rounds,
	}
	r.match = nil

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.store.SaveGame(ctx, fg); err != nil {
			r.logger.Error("save game failed", "game_id", fg.ID, "error", err)
			return
		}
		r.logger.Info("game saved", "game_id", fg.ID)
	}()
}

func newMatchID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

func (r *Room) stopTimers() {
	for kind, t := range r.timers {
		t.Stop()
		delete(r.timers, kind)
	}
}

func (r *Room) publishClosed() {
	r.snapshot.Store(&Snapshot{
		ID:           r.id,
		JoinCode:     r.joinCode,
		State:        PhaseClosed,
		PlayerCount:  0,
		HostNickname: r.label,
	})
}

func (r *Room) broadcastRoster() {
	env, _ := wsutil.NewEnvelope(wsutil.TypeRoster, rosterPayload{Players: r.engine.Roster()})
	r.broadcast(env)
}

func (r *Room) broadcast(env wsutil.Envelope) {
	for _, meta := range r.sessions {
		if !meta.session.Send(env) {
			meta.session.Kick()
		}
	}
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
	r.deliver(inboundCommand{playerID: game.PlayerID(playerID), env: env})
}

func (r *Room) NotifyDisconnect(playerID string) {
	r.deliver(disconnectCommand{playerID: game.PlayerID(playerID)})
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
	r.snapshot.Store(&Snapshot{
		ID:           r.id,
		JoinCode:     r.joinCode,
		State:        state,
		PlayerCount:  r.engine.PlayerCount(),
		HostNickname: r.label,
	})
}

type joinedPayload struct {
	PlayerID string `json:"player_id"`
	Token    string `json:"token"`
	Host     bool   `json:"host"`
}

type rosterPayload struct {
	Players []game.PlayerView `json:"players"`
}

type roundStartPayload struct {
	Round        int    `json:"round"`
	TotalRounds  int    `json:"total_rounds"`
	PanoID       string `json:"pano_id,omitempty"`
	Hint         string `json:"hint,omitempty"`
	DeadlineUnix int64  `json:"deadline_unix"`
	Seconds      int    `json:"seconds"`
}

type roundResultPayload struct {
	Round   int                `json:"round"`
	Target  game.LatLng        `json:"target"`
	Results []game.RoundResult `json:"results"`
}

type gameOverPayload struct {
	Standings []game.Standing `json:"standings"`
}

func joinedEnvelope(id game.PlayerID, token string, host bool) wsutil.Envelope {
	env, _ := wsutil.NewEnvelope(wsutil.TypeJoined, joinedPayload{
		PlayerID: string(id),
		Token:    token,
		Host:     host,
	})
	return env
}

func sendError(session *wsutil.Session, msg string) {
	env, _ := wsutil.NewEnvelope(wsutil.TypeError, map[string]string{"error": msg})
	if !session.Send(env) {
		session.Kick()
	}
}

func tokenMatches(stored, provided string) bool {
	return subtle.ConstantTimeCompare([]byte(stored), []byte(provided)) == 1
}

func findRejected(acts []game.Action, playerID game.PlayerID) (string, bool) {
	for _, a := range acts {
		if rej, ok := a.(game.RejectedAction); ok && rej.PlayerID == playerID {
			return rej.Reason, true
		}
	}
	return "", false
}

func mintTokenPair() (id game.PlayerID, token string, err error) {
	var b [8 + tokenBytes]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	hexed := hex.EncodeToString(b[:])
	split := 8 * 2
	return game.PlayerID(hexed[:split]), hexed[split:], nil
}
