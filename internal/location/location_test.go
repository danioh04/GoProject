package location

import (
	"math/rand/v2"
	"testing"

	"geoduel/internal/game"
)

func TestPoolInitialization(t *testing.T) {
	pool := New()
	if pool.Len() < 30 {
		t.Errorf("pool len = %d, want >= 30", pool.Len())
	}

	custom := []game.Location{
		{ID: "loc1", Lat: 10, Lng: 20},
		{ID: "loc2", Lat: 30, Lng: 40},
	}
	customPool := New(custom...)
	if customPool.Len() != 2 {
		t.Errorf("custom pool len = %d, want 2", customPool.Len())
	}
}

func TestPickReturnsDistinctLocations(t *testing.T) {
	pool := New()
	picked := pool.Pick(5)
	if len(picked) != 5 {
		t.Fatalf("picked %d, want 5", len(picked))
	}
	seen := make(map[string]struct{})
	for _, l := range picked {
		if _, dup := seen[l.ID]; dup {
			t.Errorf("duplicate picked id %q", l.ID)
		}
		seen[l.ID] = struct{}{}
		var zero game.Location
		if l == zero || !l.LatLng().Valid() {
			t.Errorf("invalid location: %+v", l)
		}
	}
}

func TestPickEdgeCases(t *testing.T) {
	pool := New()
	if got := pool.Pick(0); got != nil {
		t.Error("Pick(0) should be nil")
	}
	if got := pool.Pick(-5); got != nil {
		t.Error("Pick(-5) should be nil")
	}
	many := pool.Pick(20)
	if len(many) != 20 {
		t.Errorf("Pick(20) = %d, want 20", len(many))
	}
}

func TestPickerDeterministicWithSeededRand(t *testing.T) {
	pool := New()

	run := func() []string {
		rng := rand.New(rand.NewPCG(42, 42))
		picker := pool.Picker(rng)
		var out []string
		for _, l := range picker(5) {
			out = append(out, l.Title)
		}
		return out
	}

	first, second := run(), run()
	if len(first) == 0 {
		t.Fatal("no locations returned")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("seeded pickers diverged at %d: %q vs %q", i, first[i], second[i])
		}
	}
}
