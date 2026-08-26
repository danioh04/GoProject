package location

import (
	"math/rand/v2"
	"testing"

	"geoduel/internal/game"
)

func TestLoadEmbeddedPool(t *testing.T) {
	pool, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pool.Len() < 100 {
		t.Errorf("pool size = %d, want >= 100", pool.Len())
	}
}

func TestPickReturnsDistinctLocations(t *testing.T) {
	pool, _ := Load()
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
	pool, _ := Load()
	if got := pool.Pick(0); got != nil {
		t.Error("Pick(0) should be nil")
	}
	if got := pool.Pick(pool.Len() + 1); got != nil {
		t.Error("Pick(len+1) should be nil")
	}
	all := pool.Pick(pool.Len())
	if len(all) != pool.Len() {
		t.Errorf("Pick(all) = %d", len(all))
	}
}

func TestPickerDeterministicWithSeededRand(t *testing.T) {
	pool, _ := Load()

	run := func() []string {
		rng := rand.New(rand.NewPCG(42, 42))
		picker := pool.Picker(rng)
		var out []string
		for i := 0; i < 3; i++ {
			for _, l := range picker(5) {
				out = append(out, l.ID)
			}
		}
		return out
	}

	first, second := run(), run()
	if len(first) == 0 {
		t.Fatal("no ids returned")
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("seeded pickers diverged at %d: %q vs %q", i, first[i], second[i])
		}
	}
}
