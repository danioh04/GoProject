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

func (e *Engine) now() time.Time {
	return e.cfg.Clock.Now()
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
func (e *Engine) PlayerCount() int { return len(e.order) }

func (e *Engine) Roster() []Player {
	out := make([]Player, 0, len(e.order))
	for _, id := range e.order {
		if p, ok := e.players[id]; ok {
			out = append(out, *p)
		}
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

	isCreator := e.cfg.CreatorNick != "" && strings.EqualFold(ev.Nickname, e.cfg.CreatorNick)
	host := len(e.order) == 0 || isCreator

	if isCreator && len(e.order) > 0 {
		for _, pid := range e.order {
			if p, ok := e.players[pid]; ok {
				p.IsHost = false
			}
		}
	}

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

	if (e.phase == PhasePlaying || e.phase == PhaseReveal) && len(e.order) < e.cfg.MinPlayers {
		e.phase = PhaseFinished
		return []Action{
			RosterChangedAction{},
			MatchEndedAction{
				Standings: e.sortedStandings(),
				Rounds:    e.roundsHistory,
				Reason:    "insufficient_players",
			},
		}
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
			e.order = slices.Delete(e.order, i, i+1)
			break
		}
	}
	delete(e.players, id)

	if e.phase == PhaseLobby {
		delete(e.totals, id)
		for round := range e.guesses {
			delete(e.guesses[round], id)
		}
	}

	if len(e.order) > 0 {
		hasHost := false
		for _, pid := range e.order {
			if p := e.players[pid]; p != nil && p.IsHost {
				hasHost = true
				break
			}
		}
		if !hasHost {
			if p := e.players[e.order[0]]; p != nil {
				p.IsHost = true
			}
		}
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
		nick := ""
		if p != nil {
			nick = p.Nickname
		}
		res := RoundResult{PlayerID: id, Nickname: nick, Score: 0}
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

func sortResultsByScore(results []RoundResult, order []PlayerID) {
	slices.SortStableFunc(results, func(a, b RoundResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(slices.Index(order, a.PlayerID), slices.Index(order, b.PlayerID))
	})
}

func reject(id PlayerID, format string, args ...any) []Action {
	return []Action{RejectedAction{PlayerID: id, Reason: fmt.Sprintf(format, args...)}}
}
