package location

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"time"

	"scope/internal/game"
)

const (
	defaultEndpoint           = "https://maps.googleapis.com/maps/api/streetview/metadata"
	defaultSearchRadiusMeters = 50_000
	defaultJitterKm           = 5.0
	maxDiscoveryPasses        = 3
)

type Pool struct {
	apiKey     string
	radius     int
	httpClient *http.Client
	logger     *slog.Logger
	seeds      []game.Location
}

type Option func(*Pool)

// WithLogger configures a custom slog.Logger for the location pool.
func WithLogger(logger *slog.Logger) Option {
	return func(p *Pool) {
		p.logger = logger
	}
}

// New initializes a location pool configured with offline seeds and optional discovery.
func New(apiKey string, opts ...Option) *Pool {
	p := &Pool{
		apiKey:     apiKey,
		radius:     defaultSearchRadiusMeters,
		httpClient: &http.Client{Timeout: 4 * time.Second},
		logger:     slog.Default(),
		seeds:      CuratedLocations(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Pick selects n locations, attempting Google Street View discovery when an API key is configured.
func (p *Pool) Pick(n int) []game.Location {
	if n <= 0 {
		return nil
	}
	if p.apiKey == "" {
		return p.pickFromSeeds(n)
	}

	total := len(p.seeds)
	if total == 0 {
		return nil
	}

	out := make([]game.Location, 0, n)
	seen := make(map[string]struct{}, n)
	perm := mrand.Perm(total)

	for i := 0; len(out) < n && i < total*maxDiscoveryPasses; i++ {
		seed := p.seeds[perm[i%total]]
		if i > 0 && i%total == 0 {
			perm = mrand.Perm(total)
		}

		loc, err := p.discoverOne(seed.LatLng)
		if err == nil && loc.Valid() {
			if _, exists := seen[loc.ID]; !exists {
				seen[loc.ID] = struct{}{}
				out = append(out, loc)
				continue
			}
		}

		if _, exists := seen[seed.ID]; !exists {
			seen[seed.ID] = struct{}{}
			out = append(out, seed)
		}
	}

	return out
}

// Picker returns a function matching the game engine picker signature.
func (p *Pool) Picker() func(n int) []game.Location {
	return p.Pick
}

// MapName returns the human-readable identifier of the location seed set.
func (p *Pool) MapName() string {
	return "curated_world"
}

// pickFromSeeds selects n locations exclusively from offline curated seeds without network calls.
func (p *Pool) pickFromSeeds(n int) []game.Location {
	total := len(p.seeds)
	if total == 0 {
		return nil
	}

	perm := mrand.Perm(total)
	count := n
	if count > total {
		count = total
	}

	out := make([]game.Location, 0, n)
	for i := 0; i < count; i++ {
		out = append(out, p.seeds[perm[i]])
	}
	for len(out) < n {
		out = append(out, p.seeds[mrand.IntN(total)])
	}

	return out
}

type metadataResponse struct {
	Status   string `json:"status"`
	PanoID   string `json:"pano_id"`
	Location struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
}

// discoverOne queries the Google Street View metadata API for a panorama near coordinates.
func (p *Pool) discoverOne(coord game.LatLng) (game.Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	queryCoord := applyJitter(coord, defaultJitterKm)
	reqURL := fmt.Sprintf("%s?location=%.6f,%.6f&radius=%d&key=%s",
		defaultEndpoint, queryCoord.Lat, queryCoord.Lng, p.radius, url.QueryEscape(p.apiKey))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return game.Location{}, err
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return game.Location{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return game.Location{}, fmt.Errorf("metadata api status %d", resp.StatusCode)
	}

	var meta metadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return game.Location{}, fmt.Errorf("decode metadata: %w", err)
	}

	if meta.Status != "OK" || meta.PanoID == "" {
		return game.Location{}, fmt.Errorf("no panorama found: %s", meta.Status)
	}

	return game.Location{
		ID:     meta.PanoID,
		PanoID: meta.PanoID,
		LatLng: game.LatLng{Lat: meta.Location.Lat, Lng: meta.Location.Lng},
	}, nil
}

// applyJitter applies spherical coordinate displacement within a given kilometer radius.
func applyJitter(coord game.LatLng, maxKm float64) game.LatLng {
	if maxKm <= 0 {
		return coord
	}

	latDelta := (mrand.Float64()*2 - 1) * (maxKm / 111.0)
	rad := coord.Lat * math.Pi / 180.0
	cosLat := math.Cos(rad)
	if math.Abs(cosLat) < 0.01 {
		cosLat = 0.01
	}

	lngDelta := (mrand.Float64()*2 - 1) * (maxKm / (111.0 * cosLat))
	jittered := game.LatLng{
		Lat: coord.Lat + latDelta,
		Lng: coord.Lng + lngDelta,
	}

	if !jittered.Valid() {
		return coord
	}

	return jittered
}
