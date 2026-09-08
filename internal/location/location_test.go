package location

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"prism/internal/game"
	"testing"
)

func TestCuratedLocations_CountAndValidity(t *testing.T) {
	locs := CuratedLocations()
	if len(locs) != 20 {
		t.Fatalf("expected exactly 20 curated locations, got %d", len(locs))
	}

	seenID := make(map[string]bool, len(locs))
	seenPano := make(map[string]bool, len(locs))
	for _, l := range locs {
		if l.ID == "" {
			t.Errorf("location missing ID: %+v", l)
		}
		if seenID[l.ID] {
			t.Errorf("duplicate location ID %q", l.ID)
		}
		seenID[l.ID] = true

		if l.PanoID == "" {
			t.Errorf("location missing PanoID: %+v", l)
		}
		if seenPano[l.PanoID] {
			t.Errorf("duplicate PanoID %q", l.PanoID)
		}
		seenPano[l.PanoID] = true

		if !l.Valid() {
			t.Errorf("invalid coordinate for location %q: (%.4f, %.4f)", l.ID, l.Lat, l.Lng)
		}
	}
}

func TestRandomSeed(t *testing.T) {
	s := RandomSeed()
	if s.ID == "" || !s.Valid() {
		t.Fatalf("invalid random seed: %+v", s)
	}
}

func TestApplyJitter(t *testing.T) {
	origin := game.LatLng{Lat: 48.8584, Lng: 2.2945} // Paris

	// Zero jitter should return identical coordinate
	same := applyJitter(origin, 0)
	if same != origin {
		t.Fatalf("expected identical coordinate with 0 jitter, got %+v", same)
	}

	// 5km jitter test across multiple trials
	for range 50 {
		jittered := applyJitter(origin, 5.0)
		if !jittered.Valid() {
			t.Fatalf("jitter produced invalid coordinate: %+v", jittered)
		}

		distM := game.HaversineMeters(origin, jittered)
		// Max displacement in a box of ±5km lat and ±5km lng is 5 * sqrt(2) ≈ 7.07 km
		if distM > 10000 {
			t.Fatalf("jitter displacement too large: %.1f meters", distM)
		}
	}
}

func TestPool_OfflinePick(t *testing.T) {
	pool := New("") // Offline mode (no API key)

	if pool.MapName() != "curated_world" {
		t.Errorf("expected map name 'curated_world', got %q", pool.MapName())
	}

	if pool.Pick(0) != nil {
		t.Errorf("expected nil when picking 0")
	}

	picks := pool.Pick(5)
	if len(picks) != 5 {
		t.Fatalf("expected 5 picks, got %d", len(picks))
	}

	seen := make(map[string]bool, 5)
	for _, p := range picks {
		if seen[p.ID] {
			t.Errorf("duplicate location in 5-round pick: %s", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestPool_OnlinePick_WithMockServer(t *testing.T) {
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		// Return valid metadata for the first 3 queries, then fail
		if reqCount <= 3 {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"status":"OK","pano_id":"mock_pano_%d","location":{"lat":48.85,"lng":2.35}}`, reqCount)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"status":"ZERO_RESULTS"}`)
	}))
	defer server.Close()

	pool := New("mock-api-key",
		WithEndpoint(server.URL),
		WithHTTPClient(server.Client()),
	)

	picks := pool.Pick(5)
	if len(picks) != 5 {
		t.Fatalf("expected 5 picks, got %d", len(picks))
	}

	seen := make(map[string]bool, 5)
	for _, p := range picks {
		if seen[p.ID] {
			t.Errorf("duplicate location in online pick: %s", p.ID)
		}
		seen[p.ID] = true
	}
}
