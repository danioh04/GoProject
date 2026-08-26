package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"geoduel/internal/api"
	"geoduel/internal/hub"
	"geoduel/internal/room"
	"geoduel/internal/store"
)

type fakeStats struct {
	stats  []store.LocationStat
	detail *store.GameDetail
}

func (f *fakeStats) HardestLocations(ctx context.Context, limit int) ([]store.LocationStat, error) {
	if limit > len(f.stats) {
		limit = len(f.stats)
	}
	return f.stats[:limit], nil
}

func (f *fakeStats) GameDetail(ctx context.Context, id string) (*store.GameDetail, error) {
	if f.detail == nil || id != f.detail.ID {
		return nil, store.ErrNotFound
	}
	return f.detail, nil
}

func TestStatsDisabledWithoutPersistence(t *testing.T) {
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), nil))
	defer srv.Close()

	for _, path := range []string{"/v1/stats/hardest", "/v1/games/whatever"} {
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s status = %d, want 503", path, resp.StatusCode)
		}
	}
}

func TestHardestLocationsEndpoint(t *testing.T) {
	fake := &fakeStats{
		stats: []store.LocationStat{
			{LocationID: "yakutsk", Samples: 7, AvgMissKM: 4210.5},
			{LocationID: "paris", Samples: 12, AvgMissKM: 830.2},
		},
	}
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), fake))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/v1/stats/hardest?limit=1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var got struct {
		Locations []struct {
			LocationID string  `json:"location_id"`
			Samples    int64   `json:"samples"`
			AvgMissKM  float64 `json:"avg_miss_km"`
		} `json:"locations"`
	}
	json.Unmarshal(body, &got)
	if len(got.Locations) != 1 || got.Locations[0].LocationID != "yakutsk" {
		t.Errorf("limit not applied or wrong data: %s", body)
	}

	bad, err := srv.Client().Get(srv.URL + "/v1/stats/hardest?limit=99")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("limit=99 status = %d, want 400", bad.StatusCode)
	}
}

func TestGameDetailEndpoint(t *testing.T) {
	fake := &fakeStats{
		detail: &store.GameDetail{
			ID:          "game-1",
			TotalRounds: 5,
			Standings: []store.StandingRow{
				{PlayerID: "p1", Nickname: "alice", TotalScore: 12345, Placement: 1},
			},
		},
	}
	srv := httptest.NewServer(api.New(testLogger(), hub.New(testLogger(), room.Options{}), fake))
	defer srv.Close()

	okResp, err := srv.Client().Get(srv.URL + "/v1/games/game-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(okResp.Body)
	okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"placement":1`) {
		t.Errorf("detail response wrong: %d %s", okResp.StatusCode, body)
	}

	missing, err := srv.Client().Get(srv.URL + "/v1/games/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("missing game status = %d, want 404", missing.StatusCode)
	}
}
