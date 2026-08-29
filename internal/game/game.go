package game

import (
	"time"
)

type PlayerID string

type Player struct {
	PlayerID PlayerID `json:"player_id"`
	Nickname string   `json:"nickname"`
	IsHost   bool     `json:"is_host"`
}

type Phase string

const (
	PhaseLobby    Phase = "lobby"
	PhasePlaying  Phase = "playing"
	PhaseReveal   Phase = "reveal"
	PhaseFinished Phase = "finished"
)

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (p LatLng) Valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

type Location struct {
	ID     string `json:"id"`
	PanoID string `json:"pano_id"`
	LatLng
}

type RoundResult struct {
	PlayerID  PlayerID `json:"player_id"`
	Nickname  string   `json:"nickname"`
	Guess     *LatLng  `json:"guess,omitempty"`
	DistanceM float64  `json:"distance_m"`
	Score     int      `json:"score"`
}

type Standing struct {
	PlayerID PlayerID `json:"player_id"`
	Nickname string   `json:"nickname"`
	Total    int      `json:"total"`
}

type FinishedRound struct {
	Round      int           `json:"round"`
	LocationID string        `json:"location_id"`
	Target     LatLng        `json:"target"`
	Results    []RoundResult `json:"results"`
}

type TimerKind string

const (
	TimerDeadline TimerKind = "deadline"
	TimerReveal   TimerKind = "reveal"
)

type TimerTag struct {
	Kind  TimerKind
	Round int
}

type Config struct {
	Rounds     int
	RoundTime  time.Duration
	RevealTime time.Duration
	MaxPlayers int
	MinPlayers int
	MaxScore   int
}

func DefaultConfig() Config {
	return Config{
		Rounds:     5,
		RoundTime:  60 * time.Second,
		RevealTime: 10 * time.Second,
		MaxPlayers: 8,
		MinPlayers: 2,
		MaxScore:   5000,
	}
}

type Event interface{ kind() eventType }

type eventType string

const (
	evJoin    eventType = "join"
	evLeave   eventType = "leave"
	evStart   eventType = "start"
	evGuess   eventType = "guess"
	evTimeout eventType = "timeout"
)

type JoinEvent struct {
	PlayerID PlayerID
	Nickname string
}

func (e JoinEvent) kind() eventType { return evJoin }

type LeaveEvent struct{ PlayerID PlayerID }

func (e LeaveEvent) kind() eventType { return evLeave }

type StartEvent struct{ PlayerID PlayerID }

func (e StartEvent) kind() eventType { return evStart }

type GuessEvent struct {
	PlayerID PlayerID
	Guess    LatLng
}

func (e GuessEvent) kind() eventType { return evGuess }

type TimeoutEvent struct{ Tag TimerTag }

func (e TimeoutEvent) kind() eventType { return evTimeout }

type Action interface{ actionKind() actionType }

type actionType string

const (
	actPlayerJoined   actionType = "player_joined"
	actRejected       actionType = "rejected"
	actRosterChanged  actionType = "roster_changed"
	actMatchStarted   actionType = "match_started"
	actRoundStarted   actionType = "round_started"
	actGuessAccepted  actionType = "guess_accepted"
	actRoundRevealed  actionType = "round_revealed"
	actMatchEnded     actionType = "match_ended"
	actTimerScheduled actionType = "timer_scheduled"
	actRoomEmpty      actionType = "room_empty"
)

type PlayerJoinedAction struct {
	PlayerID PlayerID
	Host     bool
}

func (a PlayerJoinedAction) actionKind() actionType { return actPlayerJoined }

type RejectedAction struct {
	PlayerID PlayerID
	Reason   string
}

func (a RejectedAction) actionKind() actionType { return actRejected }

type RosterChangedAction struct{}

func (a RosterChangedAction) actionKind() actionType { return actRosterChanged }

type MatchStartedAction struct{ TotalRounds int }

func (a MatchStartedAction) actionKind() actionType { return actMatchStarted }

type RoundStartedAction struct {
	Round        int
	TotalRounds  int
	PanoID       string
	Deadline     time.Time
	RoundSeconds int
}

func (a RoundStartedAction) actionKind() actionType { return actRoundStarted }

type GuessAcceptedAction struct {
	PlayerID PlayerID
	Round    int
}

func (a GuessAcceptedAction) actionKind() actionType { return actGuessAccepted }

type RoundRevealedAction struct {
	Round   int
	Target  LatLng
	Results []RoundResult
}

func (a RoundRevealedAction) actionKind() actionType { return actRoundRevealed }

type MatchEndedAction struct {
	Standings []Standing
	Rounds    []FinishedRound
}

func (a MatchEndedAction) actionKind() actionType { return actMatchEnded }

type TimerScheduledAction struct {
	Tag   TimerTag
	Delay time.Duration
}

func (a TimerScheduledAction) actionKind() actionType { return actTimerScheduled }

type RoomEmptyAction struct{}

func (a RoomEmptyAction) actionKind() actionType { return actRoomEmpty }
