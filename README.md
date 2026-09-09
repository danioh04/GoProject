# Scope

A GeoGuessr-style multiplayer backend in Go.

## Run

With Docker:

```bash
docker compose up
```

Locally:

```bash
go run ./cmd/server
```

## Test & Lint

```bash
make test
make lint
```

## Load Test

```bash
go run ./cmd/loadbot -rooms 2 -per-room 2 -v
```
