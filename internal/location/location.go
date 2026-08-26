// Package location loads and draws from the curated panorama pool.
package location

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"geoduel/internal/game"
)

//go:embed locations.json
var raw []byte

type Pool struct {
	locations []game.Location
}

func Load() (*Pool, error) {
	var locs []game.Location
	if err := json.Unmarshal(raw, &locs); err != nil {
		return nil, fmt.Errorf("parse locations.json: %w", err)
	}
	seen := make(map[string]struct{}, len(locs))
	for i, l := range locs {
		if l.ID == "" {
			return nil, fmt.Errorf("location %d missing id", i)
		}
		if _, dup := seen[l.ID]; dup {
			return nil, fmt.Errorf("duplicate location id %q", l.ID)
		}
		seen[l.ID] = struct{}{}
		if !l.LatLng().Valid() {
			return nil, fmt.Errorf("location %q has invalid coordinates", l.ID)
		}
	}
	return &Pool{locations: locs}, nil
}

func (p *Pool) Len() int { return len(p.locations) }

func (p *Pool) Pick(n int) []game.Location {
	return p.pickWith(nil, n)
}

func (p *Pool) pickWith(rnd *rand.Rand, n int) []game.Location {
	if n <= 0 || n > len(p.locations) {
		return nil
	}
	shuffled := make([]game.Location, len(p.locations))
	copy(shuffled, p.locations)
	if rnd != nil {
		rnd.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
	} else {
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
	}
	return shuffled[:n]
}

func (p *Pool) Picker(rnd *rand.Rand) func(n int) []game.Location {
	return func(n int) []game.Location {
		return p.pickWith(rnd, n)
	}
}
