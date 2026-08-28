package store

import (
	"context"
	"geoduel/internal/game"
	"geoduel/internal/room"
	"os"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("GEODUEL_TEST_DATABASE")
	if url == "" {
		t.Skip("GEODUEL_TEST_DATABASE not set; skipping postgres integration test")
	}
	ctx := context.Background()
	st, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func cleanupGames(t *testing.T, st *Store, prefix string) {
	t.Helper()
	_, err := st.pool.Exec(context.Background(), `DELETE FROM games WHERE id LIKE $1`, prefix+"%")
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func TestSaveAndLoadGameRoundtrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	cleanupGames(t, st, "itest-rt")

	guessA := game.LatLng{Lat: 48.85, Lng: 2.35}
	finished := room.FinishedGame{
		ID:          "itest-rt",
		CreatedAt:   time.Unix(1700000000, 0).UTC(),
		TotalRounds: 2,
		Standings: []game.Standing{
			{PlayerID: "p-b", Nickname: "bob", Total: 5000},
			{PlayerID: "p-a", Nickname: "alice", Total: 2100},
		},
		Rounds: []room.FinishedRound{
			{
				Round:      1,
				LocationID: "paris",
				Target:     game.LatLng{Lat: 48.8566, Lng: 2.3522},
				Results: []game.RoundResult{
					{PlayerID: "p-b", Nickname: "bob", Guess: &guessA, DistanceM: 812.5, Score: 4900},
					{PlayerID: "p-a", Nickname: "alice", Score: 0},
				},
			},
			{
				Round:      2,
				LocationID: "tokyo",
				Target:     game.LatLng{Lat: 35.6762, Lng: 139.6503},
				Results: []game.RoundResult{
					{PlayerID: "p-a", Nickname: "alice", Guess: &game.LatLng{Lat: -33.86, Lng: 151.2}, DistanceM: 7823000, Score: 2100},
					{PlayerID: "p-b", Nickname: "bob", Guess: &game.LatLng{Lat: 35.7, Lng: 139.6}, DistanceM: 7300, Score: 4980},
				},
			},
		},
	}

	if err := st.SaveGame(ctx, finished); err != nil {
		t.Fatalf("SaveGame: %v", err)
	}

	detail, err := st.GameDetail(ctx, "itest-rt")
	if err != nil {
		t.Fatalf("GameDetail: %v", err)
	}

	if detail.TotalRounds != 2 || detail.CreatedAt.Unix() != 1700000000 {
		t.Errorf("game header wrong: %+v", detail)
	}
	if len(detail.Standings) != 2 || detail.Standings[0].Placement != 1 ||
		detail.Standings[0].Nickname != "bob" || detail.Standings[1].Placement != 2 {
		t.Errorf("standings wrong: %+v", detail.Standings)
	}
	if len(detail.Rounds) != 2 {
		t.Fatalf("rounds loaded = %d, want 2", len(detail.Rounds))
	}
	r1 := detail.Rounds[0]
	if r1.LocationID != "paris" || r1.Target.Lat != 48.8566 || len(r1.Results) != 2 {
		t.Errorf("round 1 wrong: %+v", r1)
	}
	if r1.Results[0].Nickname != "bob" || r1.Results[1].Nickname != "alice" {
		t.Errorf("round 1 nicknames wrong: %q, %q", r1.Results[0].Nickname, r1.Results[1].Nickname)
	}
	if r1.Results[1].Guess != nil || r1.Results[1].Score != 0 {
		t.Errorf("missing guess should load as null: %+v", r1.Results[1])
	}
	if r1.Results[0].DistanceM != 812.5 {
		t.Errorf("distance mismatch: %+v", r1.Results[0])
	}
}

func TestHardestLocationsRanking(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	cleanupGames(t, st, "itest-hard")

	mk := func(id, locID string, distances ...float64) room.FinishedGame {
		fr := room.FinishedRound{
			Round:      1,
			LocationID: locID,
			Target:     game.LatLng{Lat: 0, Lng: 0},
			Results:    []game.RoundResult{},
		}
		for i, d := range distances {
			g := game.LatLng{Lat: d / 111000, Lng: 0}
			fr.Results = append(fr.Results, game.RoundResult{
				PlayerID:  game.PlayerID(string(rune('a' + i))),
				Nickname:  string(rune('a' + i)),
				Guess:     &g,
				DistanceM: d,
			})
		}
		return room.FinishedGame{
			ID:          id,
			CreatedAt:   time.Now().UTC(),
			TotalRounds: 1,
			Standings:   []game.Standing{{PlayerID: "x", Nickname: "x"}},
			Rounds:      []room.FinishedRound{fr},
		}
	}

	hard := mk("itest-hard-1", "itest-loc-hard", 3_000_000, 3_100_000)
	easy := mk("itest-hard-2", "itest-loc-easy", 90_000, 110_000)
	for _, g := range []room.FinishedGame{hard, easy} {
		if err := st.SaveGame(ctx, g); err != nil {
			t.Fatalf("seed %s: %v", g.ID, err)
		}
	}

	stats, err := st.HardestLocations(ctx, 10)
	if err != nil {
		t.Fatalf("HardestLocations: %v", err)
	}

	hardIdx, easyIdx := -1, -1
	for i := range stats {
		switch stats[i].LocationID {
		case "itest-loc-hard":
			hardIdx = i
			if stats[i].Samples != 2 || stats[i].AvgMissKM < 2900 {
				t.Errorf("hard stat wrong: %+v", stats[i])
			}
		case "itest-loc-easy":
			easyIdx = i
			if stats[i].Samples != 2 || stats[i].AvgMissKM > 150 {
				t.Errorf("easy stat wrong: %+v", stats[i])
			}
		}
	}
	if hardIdx == -1 || easyIdx == -1 {
		t.Fatalf("seeded locations missing from stats: %+v", stats)
	}
	if hardIdx > easyIdx {
		t.Errorf("harder location should rank first")
	}
}
