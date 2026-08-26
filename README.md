# GeoDuel

A multiplayer GeoGuessr-style game backend in Go. Rooms of 2–8 players join by code, get the same
location clue each round, drop a pin on a real world map, and are scored on distance. The server is
authoritative: clients only ever say where they guessed — never what the answer or score is.

Built as a portfolio project to demonstrate idiomatic Go: actor-per-room concurrency, WebSocket
transport with backpressure, deterministic clock-injected game logic, and race-clean operation
under concurrent load.

## Quickstart

```bash
docker compose up        # app + postgres, http://localhost:8080
```

or without Docker:

```bash
go run ./cmd/server      # set DATABASE_URL to enable persistence (optional)
```

Open two browser windows, create a room in one, join with the code in the other, and start.

## Architecture

```
Browser client (Leaflet + OSM tiles)
   │  REST: create/join room          WS: gameplay events
   ▼                                  ▼
Go HTTP server ──► Hub (join code → room)
                      └─ goroutine + inbox channel per room (actor)
                           ├─ internal/game: pure FSM, injected clock, no I/O
                           ├─ timers re-enter the actor through its channel
                           └─ bounded outbound buffers; slow clients are kicked
Postgres (finished games) ── optional, enabled via DATABASE_URL
```

Game rules live entirely in `internal/game` as an event → action state machine with an injectable
clock, so the whole ruleset is tested instantly with fake time and zero sockets.

## HTTP API

| Method | Path | Description |
|---|---|---|
| POST | `/v1/rooms` | Create room `{"nickname":"..."}` → join code |
| GET | `/v1/rooms/{code}` | Room preview |
| GET | `/v1/ws?code=&name=` | WebSocket attach |
| GET | `/v1/stats/hardest` | Avg miss distance per location |
| GET | `/v1/games/{id}` | Finished match detail |
| GET | `/healthz` | Liveness |

WebSocket messages are versioned JSON envelopes (`{"v":1,"type":...,"payload":...}`). Client sends
`guess`, `start_game`; server sends `joined`, `roster`, `round_start`, `round_result`,
`game_over`. Connection lifecycle and keep-alives use standard RFC 6455 control frames.

## Configuration

| Env | Default | Meaning |
|---|---|---|
| ADDR | :8080 | Listen address |
| DATABASE_URL | *(empty)* | Postgres DSN; empty disables persistence |
| MAX_ROOM_SIZE | 8 | Players per room |
| ROUNDS / ROUND_SECONDS / REVEAL_SECONDS | 5 / 60 / 10 | Match timing |
| LOG_LEVEL / LOG_FORMAT | info / text | slog settings (`json` also supported) |
| DEBUG_ADDR | *(empty)* | Serve expvar metrics + pprof (e.g. `:6060`) |

## Load testing

```bash
go run ./cmd/loadbot -addr localhost:8080 -rooms 40 -per-room 5
```

Spawns concurrent bot rooms that play full matches and report failures.

## Testing

```bash
make test        # unit + integration (DB tests skip without GEODUEL_TEST_DATABASE)
make bench       # micro-benchmarks (0-alloc math + high-throughput FSM)
GEODUEL_TEST_DATABASE=postgres://geoduel:geoduel@localhost:5432/geoduel?sslmode=disable \
  go test ./internal/store/
make test-race   # race detector (CI runs this on every push)
```
