package room

import (
	"sync"
	"sync/atomic"
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

type Room struct {
	id           string
	joinCode     string
	hostNickname string

	commands  chan struct{}
	done      chan struct{}
	snapshot  atomic.Pointer[Snapshot]
	closeOnce sync.Once
}

func Start(id, joinCode, hostNickname string) *Room {
	r := &Room{
		id:           id,
		joinCode:     joinCode,
		hostNickname: hostNickname,
		commands:     make(chan struct{}, 16),
		done:         make(chan struct{}),
	}
	r.publish(PhaseLobby, 0)
	go r.run()
	return r
}

func (r *Room) run() {
	defer func() {
		r.publish(PhaseClosed, 0)
		close(r.done)
	}()
	for range r.commands {
	}
}

func (r *Room) Close() {
	r.closeOnce.Do(func() {
		close(r.commands)
	})
}

func (r *Room) Done() <-chan struct{} {
	return r.done
}

func (r *Room) Snapshot() Snapshot {
	return *r.snapshot.Load()
}

func (r *Room) publish(state Phase, playerCount int) {
	r.snapshot.Store(&Snapshot{
		ID:           r.id,
		JoinCode:     r.joinCode,
		State:        state,
		PlayerCount:  playerCount,
		HostNickname: r.hostNickname,
	})
}
