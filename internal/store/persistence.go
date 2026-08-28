package store

import (
	"context"
	"errors"
	"fmt"
	"geoduel/internal/game"
	"geoduel/internal/room"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

type LocationStat struct {
	LocationID string  `json:"location_id"`
	Samples    int64   `json:"samples"`
	AvgMissKM  float64 `json:"avg_miss_km"`
}

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

func (s *Store) SaveGame(ctx context.Context, g room.FinishedGame) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

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

func (s *Store) HardestLocations(ctx context.Context, limit int) ([]LocationStat, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT r.location_id,
		        COUNT(*) AS samples,
		        AVG(g.distance_m) / 1000.0 AS avg_miss_km
		 FROM guesses g
		 JOIN rounds r ON r.game_id = g.game_id AND r.round = g.round
		 WHERE g.lat IS NOT NULL
		 GROUP BY r.location_id
		 ORDER BY avg_miss_km DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query hardest locations: %w", err)
	}
	defer rows.Close()

	out := []LocationStat{}
	for rows.Next() {
		var stat LocationStat
		if err := rows.Scan(&stat.LocationID, &stat.Samples, &stat.AvgMissKM); err != nil {
			return nil, fmt.Errorf("scan location stat: %w", err)
		}
		out = append(out, stat)
	}
	return out, rows.Err()
}

func (s *Store) GameDetail(ctx context.Context, id string) (*GameDetail, error) {
	detail := &GameDetail{Standings: []StandingRow{}, Rounds: []game.FinishedRound{}}

	err := s.pool.QueryRow(ctx,
		`SELECT created_at, total_rounds FROM games WHERE id = $1`, id).
		Scan(&detail.CreatedAt, &detail.TotalRounds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load game: %w", err)
	}
	detail.ID = id

	rows, err := s.pool.Query(ctx,
		`SELECT player_id, nickname, total_score, placement
		 FROM game_players WHERE game_id = $1 ORDER BY placement`, id)
	if err != nil {
		return nil, fmt.Errorf("load standings: %w", err)
	}
	nickByID := make(map[string]string)
	for rows.Next() {
		var row StandingRow
		var pid string
		if err := rows.Scan(&pid, &row.Nickname, &row.TotalScore, &row.Placement); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan standing: %w", err)
		}
		row.PlayerID = pid
		nickByID[pid] = row.Nickname
		detail.Standings = append(detail.Standings, row)
	}
	rows.Close()
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

	indexByRound := map[int]int{}
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
		var round int
		var pid string
		var score int
		var lat, lng, dist *float64
		if err := guessRows.Scan(&round, &pid, &lat, &lng, &dist, &score); err != nil {
			return nil, fmt.Errorf("scan guess: %w", err)
		}
		idx, ok := indexByRound[round]
		if !ok {
			continue
		}
		res := game.RoundResult{
			PlayerID: game.PlayerID(pid),
			Nickname: nickByID[pid],
			Score:    score,
		}
		if lat != nil && lng != nil {
			g := game.LatLng{Lat: *lat, Lng: *lng}
			res.Guess = &g
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
