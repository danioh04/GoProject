package game

import (
	"math"
	"time"
)

type PlayerID string

type Phase string

const (
	PhaseLobby    Phase = "lobby"
	PhasePlaying  Phase = "playing"
	PhaseReveal   Phase = "reveal"
	PhaseFinished Phase = "finished"
)

type TimerKind string

const (
	TimerDeadline TimerKind = "deadline"
	TimerReveal   TimerKind = "reveal"
)

type TimerTag struct {
	Kind  TimerKind
	Round int
}

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (p LatLng) Valid() bool {
	return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lng) &&
		!math.IsInf(p.Lat, 0) && !math.IsInf(p.Lng, 0) &&
		p.Lat >= -90 && p.Lat <= 90 &&
		p.Lng >= -180 && p.Lng <= 180
}

type Location struct {
	ID     string  `json:"id"`
	PanoID string  `json:"pano_id,omitempty"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Title  string  `json:"title,omitempty"`
}

func (l Location) LatLng() LatLng {
	return LatLng{Lat: l.Lat, Lng: l.Lng}
}

type Player struct {
	ID       PlayerID
	Nickname string
	Host     bool
}

type PlayerView struct {
	PlayerID PlayerID `json:"player_id"`
	Nickname string   `json:"nickname"`
	IsHost   bool     `json:"is_host"`
}

type LocationRef struct {
	ID     string `json:"id"`
	PanoID string `json:"pano_id,omitempty"`
}

type RoundResult struct {
	PlayerID  PlayerID `json:"player_id"`
	Nickname  string   `json:"nickname"`
	Guess     *LatLng  `json:"guess,omitempty"`
	DistanceM float64  `json:"distance_m,omitempty"`
	Score     int      `json:"score"`
}

type Standing struct {
	PlayerID PlayerID `json:"player_id"`
	Nickname string   `json:"nickname"`
	Total    int      `json:"total"`
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

type LeaveEvent struct {
	PlayerID PlayerID
}

type StartEvent struct {
	PlayerID PlayerID
}

type GuessEvent struct {
	PlayerID PlayerID
	Guess    LatLng
}

type TimeoutEvent struct {
	Tag TimerTag
}

func (e JoinEvent) kind() eventType    { return evJoin }
func (e LeaveEvent) kind() eventType   { return evLeave }
func (e StartEvent) kind() eventType   { return evStart }
func (e GuessEvent) kind() eventType   { return evGuess }
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

type RejectedAction struct {
	PlayerID PlayerID
	Reason   string
}

type RosterChangedAction struct{}

type MatchStartedAction struct {
	TotalRounds int
}

type RoundStartedAction struct {
	Round        int
	TotalRounds  int
	Location     LocationRef
	Deadline     time.Time
	RoundSeconds int
}

type GuessAcceptedAction struct {
	PlayerID PlayerID
	Round    int
}

type RoundRevealedAction struct {
	Round   int
	Target  LatLng
	Results []RoundResult
}

type FinishedRound struct {
	Number     int           `json:"number"`
	LocationID string        `json:"location_id"`
	Target     LatLng        `json:"target"`
	Results    []RoundResult `json:"results"`
}

type MatchEndedAction struct {
	Standings []Standing
	Rounds    []FinishedRound
}

type TimerScheduledAction struct {
	Tag   TimerTag
	Delay time.Duration
}

type RoomEmptyAction struct{}

func (a PlayerJoinedAction) actionKind() actionType   { return actPlayerJoined }
func (a RejectedAction) actionKind() actionType       { return actRejected }
func (a RosterChangedAction) actionKind() actionType  { return actRosterChanged }
func (a MatchStartedAction) actionKind() actionType   { return actMatchStarted }
func (a RoundStartedAction) actionKind() actionType   { return actRoundStarted }
func (a GuessAcceptedAction) actionKind() actionType  { return actGuessAccepted }
func (a RoundRevealedAction) actionKind() actionType  { return actRoundRevealed }
func (a MatchEndedAction) actionKind() actionType     { return actMatchEnded }
func (a TimerScheduledAction) actionKind() actionType { return actTimerScheduled }
func (a RoomEmptyAction) actionKind() actionType      { return actRoomEmpty }
