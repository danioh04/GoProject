package game

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type Engine struct {
	cfg           Config
	pick          func(int) []Location
	phase         Phase
	players       map[PlayerID]*Player
	order         []PlayerID
	round         int
	locations     []Location
	guesses       map[int]map[PlayerID]LatLng
	totals        map[PlayerID]int
	roundsHistory []FinishedRound
}

// Initializes a new game state engine
func New(cfg Config, pick func(n int) []Location) *Engine {
	if cfg.Clock == nil {
		cfg.Clock = RealClock{}
	}

	return &Engine{
		cfg:           cfg,
		pick:          pick,
		phase:         PhaseLobby,
		players:       make(map[PlayerID]*Player),
		guesses:       make(map[int]map[PlayerID]LatLng),
		totals:        make(map[PlayerID]int),
		roundsHistory: make([]FinishedRound, 0, cfg.Rounds),
	}
}

// Dispatches an event to the appropriate state handler and returns resulting actions
func (e *Engine) Apply(ev Event) []Action {
	switch t := ev.(type) {
	case JoinEvent:
		return e.applyJoin(t)
	case LeaveEvent:
		return e.applyLeave(t)
	case StartEvent:
		return e.applyStart(t)
	case GuessEvent:
		return e.applyGuess(t)
	case TimeoutEvent:
		return e.applyTimeout(t)
	default:
		return nil
	}
}

// Returns the current lifecycle phase of the game
func (e *Engine) Phase() Phase {
	return e.phase
}

// Returns the number of active players currently in the game
func (e *Engine) PlayerCount() int {
	return len(e.order)
}

// Returns all registered players in the order they joined
func (e *Engine) Roster() []Player {
	out := make([]Player, 0, len(e.order))
	for _, id := range e.order {
		if p, ok := e.players[id]; ok {
			out = append(out, *p)
		}
	}

	return out
}

// Validates and records a new player joining the lobby
func (e *Engine) applyJoin(ev JoinEvent) []Action {
	if e.phase != PhaseLobby {
		return reject(ev.PlayerID, "match already started")
	}
	if len(e.order) >= e.cfg.MaxPlayers {
		return reject(ev.PlayerID, "room full")
	}
	for _, id := range e.order {
		if strings.EqualFold(e.players[id].Nickname, ev.Nickname) {
			return reject(ev.PlayerID, "nickname already taken")
		}
	}

	isCreator := e.cfg.CreatorNickname != "" && strings.EqualFold(ev.Nickname, e.cfg.CreatorNickname)
	host := len(e.order) == 0 || isCreator
	if isCreator && len(e.order) > 0 {
		for _, pid := range e.order {
			if p, ok := e.players[pid]; ok {
				p.IsHost = false
			}
		}
	}

	e.players[ev.PlayerID] = &Player{
		PlayerID: ev.PlayerID,
		Nickname: ev.Nickname,
		IsHost:   host,
	}
	e.order = append(e.order, ev.PlayerID)

	return []Action{
		PlayerJoinedAction{
			PlayerID: ev.PlayerID,
			Host:     host,
		},
		RosterChangedAction{},
	}
}

// Processes player disconnection and adjusts host or triggers game end if needed
func (e *Engine) applyLeave(ev LeaveEvent) []Action {
	if _, ok := e.players[ev.PlayerID]; !ok {
		return nil
	}

	e.removePlayer(ev.PlayerID)
	if len(e.order) == 0 {
		return []Action{RoomEmptyAction{}}
	}

	if (e.phase == PhasePlaying || e.phase == PhaseReveal) && len(e.order) < e.cfg.MinPlayers {
		e.phase = PhaseFinished
		return []Action{
			RosterChangedAction{},
			GameEndedAction{
				Standings: e.sortedStandings(),
				Rounds:    e.roundsHistory,
				Reason:    "insufficient_players",
			},
		}
	}

	actions := []Action{RosterChangedAction{}}
	return append(actions, e.maybeReveal()...)
}

// Validates match prerequisites and initiates round one
func (e *Engine) applyStart(ev StartEvent) []Action {
	p, ok := e.players[ev.PlayerID]
	switch {
	case !ok:
		return reject(ev.PlayerID, "unknown player")
	case !p.IsHost:
		return reject(ev.PlayerID, "only the host can start the match")
	case e.phase != PhaseLobby:
		return reject(ev.PlayerID, "match already started")
	case len(e.order) < e.cfg.MinPlayers:
		return reject(ev.PlayerID, "need at least %d players to start", e.cfg.MinPlayers)
	}

	e.locations = e.pick(e.cfg.Rounds)
	if len(e.locations) < e.cfg.Rounds {
		return reject(ev.PlayerID, "server has no locations available")
	}

	return append([]Action{GameStartedAction{
		TotalRounds: e.cfg.Rounds}},
		e.beginRound(1)...)
}

// Records a guess for the active round and triggers reveal
func (e *Engine) applyGuess(ev GuessEvent) []Action {
	if _, ok := e.players[ev.PlayerID]; !ok {
		return reject(ev.PlayerID, "unknown player")
	}
	if e.phase != PhasePlaying {
		return reject(ev.PlayerID, "not accepting guesses right now")
	}
	if !ev.Guess.Valid() {
		return reject(ev.PlayerID, "invalid coordinates")
	}

	e.guesses[e.round][ev.PlayerID] = ev.Guess
	actions := []Action{GuessAcceptedAction{
		PlayerID: ev.PlayerID,
		Round:    e.round,
	}}
	return append(actions, e.maybeReveal()...)
}

// Processes timer expirations for round deadlines and reveal delays
func (e *Engine) applyTimeout(ev TimeoutEvent) []Action {
	switch ev.Tag.Kind {
	case TimerDeadline:
		if e.phase == PhasePlaying && ev.Tag.Round == e.round {
			return e.revealNow()
		}
	case TimerReveal:
		if e.phase == PhaseReveal && ev.Tag.Round == e.round {
			return e.advanceRound()
		}
	}
	return nil
}

// Deletes player state and reassigns host if the original host left
func (e *Engine) removePlayer(id PlayerID) {
	if idx := slices.Index(e.order, id); idx != -1 {
		e.order = slices.Delete(e.order, idx, idx+1)
	}

	delete(e.players, id)
	if e.phase == PhaseLobby {
		delete(e.totals, id)
		for round := range e.guesses {
			delete(e.guesses[round], id)
		}
	}

	if len(e.order) > 0 {
		hasHost := slices.ContainsFunc(e.order, func(pid PlayerID) bool {
			p := e.players[pid]
			return p != nil && p.IsHost
		})
		if !hasHost {
			if p := e.players[e.order[0]]; p != nil {
				p.IsHost = true
			}
		}
	}
}

// Returns the current time using the engine's injected clock
func (e *Engine) now() time.Time {
	return e.cfg.Clock.Now()
}

// Sets up state for a new round and schedules its deadline timer
func (e *Engine) beginRound(round int) []Action {
	e.round = round
	e.phase = PhasePlaying
	e.guesses[round] = make(map[PlayerID]LatLng)

	loc := e.locations[round-1]
	deadline := e.now().Add(e.cfg.RoundTime)

	return []Action{
		RoundStartedAction{
			Round:        round,
			TotalRounds:  e.cfg.Rounds,
			PanoID:       loc.PanoID,
			Deadline:     deadline,
			RoundSeconds: int(math.Ceil(e.cfg.RoundTime.Seconds())),
		},
		TimerScheduledAction{Tag: TimerTag{Kind: TimerDeadline, Round: round}, Delay: e.cfg.RoundTime},
	}
}

// Checks whether all active players have submitted guesses for the current round
func (e *Engine) maybeReveal() []Action {
	if e.phase != PhasePlaying {
		return nil
	}

	for _, id := range e.order {
		if _, ok := e.guesses[e.round][id]; !ok {
			return nil
		}
	}

	return e.revealNow()
}

// Calculates scores for the current round, updates totals, and transitions to reveal phase
func (e *Engine) revealNow() []Action {
	target := e.locations[e.round-1].LatLng
	results := make([]RoundResult, 0, len(e.order))

	for _, id := range e.order {
		p := e.players[id]
		nick := ""
		if p != nil {
			nick = p.Nickname
		}

		res := RoundResult{PlayerID: id, Nickname: nick, Score: 0}
		if g, ok := e.guesses[e.round][id]; ok {
			res.Guess = &g
			res.DistanceM = HaversineMeters(g, target)
			res.Score = Score(g, target, e.cfg.MaxScore)
			e.totals[id] += res.Score
		}
		results = append(results, res)
	}

	sortResultsByScore(results)

	loc := e.locations[e.round-1]
	resultsCopy := make([]RoundResult, len(results))
	copy(resultsCopy, results)

	e.roundsHistory = append(e.roundsHistory, FinishedRound{
		Round:      e.round,
		LocationID: loc.ID,
		Target:     target,
		Results:    resultsCopy,
	})
	e.phase = PhaseReveal

	return []Action{
		RoundRevealedAction{Round: e.round, Target: target, Results: results},
		TimerScheduledAction{Tag: TimerTag{Kind: TimerReveal, Round: e.round}, Delay: e.cfg.RevealTime},
	}
}

// advanceRound transitions to the next round or ends the match if all rounds are complete.
func (e *Engine) advanceRound() []Action {
	if e.round >= e.cfg.Rounds {
		e.phase = PhaseFinished
		return []Action{GameEndedAction{
			Standings: e.sortedStandings(),
			Rounds:    e.roundsHistory,
		}}
	}

	return e.beginRound(e.round + 1)
}

// sortedStandings returns final standings sorted by total score in descending order.
func (e *Engine) sortedStandings() []Standing {
	out := make([]Standing, 0, len(e.order))
	for _, id := range e.order {
		nick := ""
		if p, ok := e.players[id]; ok {
			nick = p.Nickname
		}
		out = append(out, Standing{
			PlayerID: id,
			Nickname: nick,
			Total:    e.totals[id],
		})
	}

	slices.SortStableFunc(out, func(a, b Standing) int {
		return cmp.Compare(b.Total, a.Total)
	})

	return out
}

// sortResultsByScore sorts round results in-place by score descending.
func sortResultsByScore(results []RoundResult) {
	slices.SortStableFunc(results, func(a, b RoundResult) int {
		return cmp.Compare(b.Score, a.Score)
	})
}

// reject returns a RejectedAction formatted with the provided reason.
func reject(id PlayerID, format string, args ...any) []Action {
	return []Action{RejectedAction{PlayerID: id, Reason: fmt.Sprintf(format, args...)}}
}
