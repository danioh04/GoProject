package game

import (
	"time"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

// Returns the current wall-clock time
func (RealClock) Now() time.Time {
	return time.Now()
}

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

// Checks whether latitude and longitude are within Earth's bounds
func (ll LatLng) Valid() bool {
	return ll.Lat >= -90 && ll.Lat <= 90 && ll.Lng >= -180 && ll.Lng <= 180
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
	Rounds          int
	RoundTime       time.Duration
	RevealTime      time.Duration
	MaxPlayers      int
	MinPlayers      int
	MaxScore        int
	Clock           Clock
	CreatorNickname string
}

// Provides recommended production settings for game sessions
func DefaultConfig() Config {
	return Config{
		Rounds:     5,
		RoundTime:  60 * time.Second,
		RevealTime: 10 * time.Second,
		MaxPlayers: 8,
		MinPlayers: 2,
		MaxScore:   5000,
		Clock:      RealClock{},
	}
}

type Event interface {
	kind() eventType
}

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

// Returns the discriminator for JoinEvent
func (e JoinEvent) kind() eventType {
	return evJoin
}

type LeaveEvent struct {
	PlayerID PlayerID
}

// Returns the discriminator for LeaveEvent
func (e LeaveEvent) kind() eventType {
	return evLeave
}

type StartEvent struct {
	PlayerID PlayerID
}

// Returns the discriminator for StartEvent
func (e StartEvent) kind() eventType {
	return evStart
}

type GuessEvent struct {
	PlayerID PlayerID
	Guess    LatLng
}

// Returns the discriminator for GuessEvent
func (e GuessEvent) kind() eventType {
	return evGuess
}

type TimeoutEvent struct {
	Tag TimerTag
}

// Returns the discriminator for TimeoutEvent
func (e TimeoutEvent) kind() eventType {
	return evTimeout
}

type Action interface {
	actionKind() actionType
}

type actionType string

const (
	actPlayerJoined   actionType = "player_joined"
	actRejected       actionType = "rejected"
	actRosterChanged  actionType = "roster_changed"
	actGameStarted    actionType = "game_started"
	actRoundStarted   actionType = "round_started"
	actGuessAccepted  actionType = "guess_accepted"
	actRoundRevealed  actionType = "round_revealed"
	actGameEnded      actionType = "game_ended"
	actTimerScheduled actionType = "timer_scheduled"
	actRoomEmpty      actionType = "room_empty"
)

type PlayerJoinedAction struct {
	PlayerID PlayerID
	Host     bool
}

// Returns the discriminator for PlayerJoinedAction
func (a PlayerJoinedAction) actionKind() actionType {
	return actPlayerJoined
}

type RejectedAction struct {
	PlayerID PlayerID
	Reason   string
}

// Returns the discriminator for RejectedAction
func (a RejectedAction) actionKind() actionType {
	return actRejected
}

type RosterChangedAction struct{}

// Returns the discriminator for RosterChangedAction
func (a RosterChangedAction) actionKind() actionType {
	return actRosterChanged
}

type GameStartedAction struct {
	TotalRounds int
}

// Returns the discriminator for GameStartedAction
func (a GameStartedAction) actionKind() actionType {
	return actGameStarted
}

type RoundStartedAction struct {
	Round        int
	TotalRounds  int
	PanoID       string
	Deadline     time.Time
	RoundSeconds int
}

// Returns the discriminator for RoundStartedAction
func (a RoundStartedAction) actionKind() actionType {
	return actRoundStarted
}

type GuessAcceptedAction struct {
	PlayerID PlayerID
	Round    int
}

// Returns the discriminator for GuessAcceptedAction
func (a GuessAcceptedAction) actionKind() actionType {
	return actGuessAccepted
}

type RoundRevealedAction struct {
	Round   int
	Target  LatLng
	Results []RoundResult
}

// Returns the discriminator for RoundRevealedAction
func (a RoundRevealedAction) actionKind() actionType {
	return actRoundRevealed
}

type GameEndedAction struct {
	Standings []Standing
	Rounds    []FinishedRound
	Reason    string
}

// Returns the discriminator for GameEndedAction
func (a GameEndedAction) actionKind() actionType {
	return actGameEnded
}

type TimerScheduledAction struct {
	Tag   TimerTag
	Delay time.Duration
}

// Returns the discriminator for TimerScheduledAction
func (a TimerScheduledAction) actionKind() actionType {
	return actTimerScheduled
}

type RoomEmptyAction struct{}

// Returns the discriminator for RoomEmptyAction
func (a RoomEmptyAction) actionKind() actionType {
	return actRoomEmpty
}
