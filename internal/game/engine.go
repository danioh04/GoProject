// Package game holds the pure, deterministic game state machine and scoring rules.
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
	now           func() time.Time
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

func New(cfg Config, now func() time.Time, pick func(n int) []Location) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{
		cfg:           cfg,
		now:           now,
		pick:          pick,
		phase:         PhaseLobby,
		players:       make(map[PlayerID]*Player),
		guesses:       make(map[int]map[PlayerID]LatLng),
		totals:        make(map[PlayerID]int),
		roundsHistory: make([]FinishedRound, 0, cfg.Rounds),
	}
}

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

func (e *Engine) Phase() Phase     { return e.phase }
func (e *Engine) Round() int       { return e.round }
func (e *Engine) PlayerCount() int { return len(e.order) }

func (e *Engine) Roster() []Player {
	out := make([]Player, 0, len(e.order))
	for _, id := range e.order {
		out = append(out, *e.players[id])
	}
	return out
}

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

	host := len(e.order) == 0
	e.players[ev.PlayerID] = &Player{PlayerID: ev.PlayerID, Nickname: ev.Nickname, IsHost: host}
	e.order = append(e.order, ev.PlayerID)

	return []Action{
		PlayerJoinedAction{PlayerID: ev.PlayerID, Host: host},
		RosterChangedAction{},
	}
}

func (e *Engine) applyLeave(ev LeaveEvent) []Action {
	if _, ok := e.players[ev.PlayerID]; !ok {
		return nil
	}
	e.removePlayer(ev.PlayerID)

	if len(e.order) == 0 {
		return []Action{RoomEmptyAction{}}
	}
	actions := []Action{RosterChangedAction{}}
	return append(actions, e.maybeReveal()...)
}

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
	return append([]Action{MatchStartedAction{TotalRounds: e.cfg.Rounds}}, e.beginRound(1)...)
}

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

	actions := []Action{GuessAcceptedAction{PlayerID: ev.PlayerID, Round: e.round}}
	return append(actions, e.maybeReveal()...)
}

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

func (e *Engine) removePlayer(id PlayerID) {
	for i, oid := range e.order {
		if oid == id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
	if e.phase == PhaseLobby {
		delete(e.players, id)
		delete(e.totals, id)
		for round := range e.guesses {
			delete(e.guesses[round], id)
		}
	}
	if remaining := e.order; len(remaining) > 0 && e.players[remaining[0]] != nil {
		for _, pid := range remaining {
			e.players[pid].IsHost = false
		}
		e.players[remaining[0]].IsHost = true
	}
}

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

func (e *Engine) revealNow() []Action {
	target := e.locations[e.round-1].LatLng
	results := make([]RoundResult, 0, len(e.order))
	for _, id := range e.order {
		p := e.players[id]
		res := RoundResult{PlayerID: id, Nickname: p.Nickname, Score: 0}
		if g, ok := e.guesses[e.round][id]; ok {
			res.Guess = &g
			res.DistanceM = haversineMeters(g, target)
			res.Score = Score(g, target, e.cfg.MaxScore)
			e.totals[id] += res.Score
		}
		results = append(results, res)
	}
	sortResultsByScore(results, e.order)

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

func (e *Engine) advanceRound() []Action {
	if e.round >= e.cfg.Rounds {
		e.phase = PhaseFinished
		return []Action{MatchEndedAction{
			Standings: e.sortedStandings(),
			Rounds:    e.roundsHistory,
		}}
	}
	return e.beginRound(e.round + 1)
}

func (e *Engine) sortedStandings() []Standing {
	out := make([]Standing, 0, len(e.order))
	for _, id := range e.order {
		out = append(out, Standing{
			PlayerID: id,
			Nickname: e.players[id].Nickname,
			Total:    e.totals[id],
		})
	}
	slices.SortStableFunc(out, func(a, b Standing) int {
		return cmp.Compare(b.Total, a.Total)
	})
	return out
}

func sortResultsByScore(results []RoundResult, order []PlayerID) {
	rank := make(map[PlayerID]int, len(order))
	for i, id := range order {
		rank[id] = i
	}
	slices.SortStableFunc(results, func(a, b RoundResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(rank[a.PlayerID], rank[b.PlayerID])
	})
}

func reject(id PlayerID, format string, args ...any) []Action {
	return []Action{RejectedAction{PlayerID: id, Reason: fmt.Sprintf(format, args...)}}
}
