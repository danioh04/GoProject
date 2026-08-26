package metrics

import "expvar"

var (
	RoomsActive  = expvar.NewInt("rooms_active")
	WSConns      = expvar.NewInt("ws_connections")
	GuessesTotal = expvar.NewInt("guesses_total")
)
