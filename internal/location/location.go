package location

import (
	"math/rand/v2"

	"geoduel/internal/game"
)

// Pool holds the available catalog of geographic locations for matches.
type Pool struct {
	locations []game.Location
}

// New creates a location pool initialized with curated world locations.
func New(locations ...[]game.Location) *Pool {
	locs := WorldLocations
	if len(locations) > 0 && locations[0] != nil {
		locs = locations[0]
	}
	return &Pool{
		locations: locs,
	}
}

// Len returns the number of locations available in the pool.
func (p *Pool) Len() int {
	return len(p.locations)
}

// Pick returns n randomly selected unique locations from the pool.
func (p *Pool) Pick(n int) []game.Location {
	return p.pickWith(nil, n)
}

// Picker returns a closure that picks n locations using the provided RNG.
func (p *Pool) Picker(rnd *rand.Rand) func(n int) []game.Location {
	return func(n int) []game.Location {
		return p.pickWith(rnd, n)
	}
}

func (p *Pool) pickWith(rnd *rand.Rand, n int) []game.Location {
	if n <= 0 {
		return nil
	}
	if n > len(p.locations) {
		n = len(p.locations)
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
