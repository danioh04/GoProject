# GeoDuel

A high-concurrency multiplayer GeoGuessr-style game server and WebSocket backend in Go. Rooms of 2–8 players join by code, receive identical 360° Google Street View panoramas each round, submit their coordinate guesses, and are scored based on distance using spherical trigonometry. The server is strictly authoritative: clients only submit where they guessed—the true target location is guarded server-side and never leaked until round reveal.

Built as a portfolio-grade backend showcasing idiomatic Go: actor-per-room concurrency, WebSocket streaming with backpressure, deterministic clock-injected finite state machines, PostgreSQL analytics, and race-free operation under high concurrent load.

## Quickstart

```bash
docker compose up        # server + postgres, http://localhost:8080
```

or without Docker:

```bash
go run ./cmd/server      # set DATABASE_URL to enable persistence (optional)
```

## Architecture

```text
HTTP / WebSocket Clients (Game UI, mobile clients, or bot swarms)
   │  REST: create/preview room       WS: realtime gameplay events
   ▼                                  ▼
Go HTTP Server ──► Hub (join code → room)
                      └─ goroutine + inbox channel per room (actor)
                           ├─ internal/game: pure FSM, clock-injected, zero I/O
                           ├─ timers re-enter the actor through its channel
                           └─ bounded outbound buffers; slow clients are kicked
Postgres (finished games & analytics) ── optional, enabled via DATABASE_URL
```

Game rules live entirely in `internal/game` as an event → action state machine with an injectable
clock, so the whole ruleset is tested instantly with fake time and zero sockets.

## HTTP API

| Method | Path | Description |
| --- | --- | --- |
| GET | `/` | Service metadata, status, and API routes index |
| GET | `/healthz` | Liveness and health probe |
| GET | `/v1/config` | Public client configuration (Google Maps API key) |
| POST | `/v1/rooms` | Create room `{"nickname":"..."}` → join code |
| GET | `/v1/rooms/{code}` | Room preview and player count |
| GET | `/v1/ws?code=&name=` | WebSocket connection upgrade and game session attach |
| GET | `/v1/stats/hardest` | Analytical ranking of hardest locations by average miss distance |
| GET | `/v1/games/{id}` | Finished match breakdown and round results |

WebSocket messages are versioned JSON envelopes (`{"v":1,"type":...,"payload":...}`). Client sends
`guess`, `start_game`; server sends `joined`, `roster`, `round_start`, `round_result`,
`game_over`. Connection lifecycle and keep-alives use standard RFC 6455 control frames.

## Configuration

| Env | Default | Meaning |
| --- | --- | --- |
| ADDR | :8080 | Listen address |
| DATABASE_URL | *(empty)* | Postgres DSN; empty disables persistence |
| GOOGLE_MAPS_API_KEY | *(empty)* | Google Maps API key for 360° Street View panoramas |
| MAP_FILE | *(empty)* | Path to custom GeoJSON map pack (falls back to embedded world map) |
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
