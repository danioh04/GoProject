# Prism

A GeoGuessr-style multiplayer game server and backend written in Go.

Rooms of 2–8 players join by code, receive identical 360° Google Street View panoramas each round, submit coordinate guesses, and are scored based on distance using spherical trigonometry. The server is strictly authoritative: clients only submit coordinates—the true target location is guarded server-side and never leaked until round reveal.

## Quickstart

```bash
docker compose up     # server + postgres, http://localhost:8080
```

or without Docker:

```bash
go run ./cmd/server   # set DATABASE_URL to enable persistence (optional)
```

## API

| Method | Path | Description |
| --- | --- | --- |
| GET | `/` | Service metadata, feature discovery, and API routes index |
| GET | `/healthz` | Liveness and health probe |
| POST | `/v1/rooms` | Create room `{"nickname":"..."}` → join code |
| GET | `/v1/rooms/{code}` | Room preview and player count |
| GET | `/v1/ws?code=&name=` | WebSocket connection upgrade and game session attach |
| GET | `/v1/games` | List recent finished matches |
| GET | `/v1/games/{id}` | Finished match breakdown and round results |

WebSocket messages are versioned JSON envelopes (`{"v":1,"type":...,"payload":...}`). Client sends `guess`, `start_game`; server sends `joined`, `roster`, `round_start`, `round_result`, `game_over` (including `game_id` for post-game lookup).

## Configuration

| Env | Default | Meaning |
| --- | --- | --- |
| ADDR | :8080 | Listen address |
| DATABASE_URL | *(empty)* | Postgres DSN; empty disables persistence |
| GOOGLE_MAPS_API_KEY | *(empty)* | Server-side Google Maps API key for Street View (falls back to curated seeds if unset) |
| MAX_PLAYERS | 8 | Maximum players per room |
| ROUNDS / ROUND_SECONDS / REVEAL_SECONDS | 5 / 60 / 10 | Match timing |
| LOG_LEVEL / LOG_FORMAT | info / text | slog settings (`json` also supported) |

## Load testing

```bash
go run ./cmd/loadbot -addr localhost:8080 -rooms 20 -per-room 4
```

Spawns concurrent bot rooms that connect over WebSockets, play full matches, and report failures without panics.

## Testing

```bash
make test        # unit test suite (engine deterministic clock playthrough + math)
make bench       # micro-benchmarks (0-alloc math + high-throughput FSM)
make test-race   # race detector
```
