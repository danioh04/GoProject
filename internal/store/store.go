package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"prism/internal/game"
	"prism/internal/room"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

var ErrNotFound = errors.New("not found")

type StandingRow struct {
	PlayerID   string `json:"player_id"`
	Nickname   string `json:"nickname"`
	TotalScore int    `json:"total_score"`
	Placement  int    `json:"placement"`
}

type GameDetail struct {
	ID          string               `json:"id"`
	CreatedAt   time.Time            `json:"created_at"`
	TotalRounds int                  `json:"total_rounds"`
	Standings   []StandingRow        `json:"standings"`
	Rounds      []game.FinishedRound `json:"rounds"`
}

type GameSummary struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	TotalRounds int       `json:"total_rounds"`
}

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse connection config: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) SaveGame(ctx context.Context, g room.FinishedGame) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}

	batch.Queue(
		`INSERT INTO games (id, created_at, total_rounds) VALUES ($1, $2, $3)`,
		g.ID, g.CreatedAt, g.TotalRounds,
	)

	for placement, st := range g.Standings {
		batch.Queue(
			`INSERT INTO game_players (game_id, player_id, nickname, total_score, placement)
			 VALUES ($1, $2, $3, $4, $5)`,
			g.ID, string(st.PlayerID), st.Nickname, st.Total, placement+1,
		)
	}

	for _, rnd := range g.Rounds {
		batch.Queue(
			`INSERT INTO rounds (game_id, round, location_id, target_lat, target_lng)
			 VALUES ($1, $2, $3, $4, $5)`,
			g.ID, rnd.Round, rnd.LocationID, rnd.Target.Lat, rnd.Target.Lng,
		)
	}

	for _, rnd := range g.Rounds {
		for _, res := range rnd.Results {
			var lat, lng, dist any
			if res.Guess != nil {
				lat, lng = res.Guess.Lat, res.Guess.Lng
				dist = res.DistanceM
			}
			batch.Queue(
				`INSERT INTO guesses (game_id, round, player_id, lat, lng, distance_m, score)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				g.ID, rnd.Round, string(res.PlayerID), lat, lng, dist, res.Score,
			)
		}
	}

	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("batch insert item %d: %w", i, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("close batch: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *Store) ListRecentGames(ctx context.Context, limit int) ([]GameSummary, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, created_at, total_rounds
		 FROM games
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent games: %w", err)
	}
	defer rows.Close()

	out := []GameSummary{}
	for rows.Next() {
		var g GameSummary
		if err := rows.Scan(&g.ID, &g.CreatedAt, &g.TotalRounds); err != nil {
			return nil, fmt.Errorf("scan game summary: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) GameDetail(ctx context.Context, id string) (*GameDetail, error) {
	detail := &GameDetail{ID: id}

	err := s.pool.QueryRow(ctx,
		`SELECT created_at, total_rounds FROM games WHERE id = $1`, id).
		Scan(&detail.CreatedAt, &detail.TotalRounds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load game: %w", err)
	}

	detail.Standings = make([]StandingRow, 0, 8)
	detail.Rounds = make([]game.FinishedRound, 0, detail.TotalRounds)

	rows, err := s.pool.Query(ctx,
		`SELECT player_id, nickname, total_score, placement
		 FROM game_players WHERE game_id = $1 ORDER BY placement`, id)
	if err != nil {
		return nil, fmt.Errorf("load standings: %w", err)
	}
	defer rows.Close()

	nickByID := make(map[string]string)
	for rows.Next() {
		var row StandingRow
		if err := rows.Scan(&row.PlayerID, &row.Nickname, &row.TotalScore, &row.Placement); err != nil {
			return nil, fmt.Errorf("scan standing: %w", err)
		}
		nickByID[row.PlayerID] = row.Nickname
		detail.Standings = append(detail.Standings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	roundRows, err := s.pool.Query(ctx,
		`SELECT round, location_id, target_lat, target_lng
		 FROM rounds WHERE game_id = $1 ORDER BY round`, id)
	if err != nil {
		return nil, fmt.Errorf("load rounds: %w", err)
	}
	defer roundRows.Close()

	indexByRound := make(map[int]int, detail.TotalRounds)
	for roundRows.Next() {
		var rd game.FinishedRound
		if err := roundRows.Scan(&rd.Round, &rd.LocationID, &rd.Target.Lat, &rd.Target.Lng); err != nil {
			return nil, fmt.Errorf("scan round: %w", err)
		}
		rd.Results = []game.RoundResult{}
		indexByRound[rd.Round] = len(detail.Rounds)
		detail.Rounds = append(detail.Rounds, rd)
	}
	if err := roundRows.Err(); err != nil {
		return nil, err
	}

	guessRows, err := s.pool.Query(ctx,
		`SELECT round, player_id, lat, lng, distance_m, score
		 FROM guesses WHERE game_id = $1 ORDER BY round, score DESC, player_id`, id)
	if err != nil {
		return nil, fmt.Errorf("load guesses: %w", err)
	}
	defer guessRows.Close()

	for guessRows.Next() {
		var round, score int
		var pid string
		var lat, lng, dist *float64
		if err := guessRows.Scan(&round, &pid, &lat, &lng, &dist, &score); err != nil {
			return nil, fmt.Errorf("scan guess: %w", err)
		}
		idx, ok := indexByRound[round]
		if !ok {
			continue
		}

		nick := nickByID[pid]
		if nick == "" {
			nick = "[disconnected]"
		}

		res := game.RoundResult{
			PlayerID: game.PlayerID(pid),
			Nickname: nick,
			Score:    score,
		}
		if lat != nil && lng != nil {
			res.Guess = &game.LatLng{Lat: *lat, Lng: *lng}
		}
		if dist != nil {
			res.DistanceM = *dist
		}
		detail.Rounds[idx].Results = append(detail.Rounds[idx].Results, res)
	}
	if err := guessRows.Err(); err != nil {
		return nil, err
	}

	return detail, nil
}
