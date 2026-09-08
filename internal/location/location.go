package location

import (
	"context"
	"encoding/json"
	"fmt"
	"prism/internal/game"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultEndpoint     = "https://maps.googleapis.com/maps/api/streetview/metadata"
	defaultSearchRadius = 50000
	defaultJitterKm     = 5.0
)

type Pool struct {
	apiKey     string
	endpoint   string
	radius     int
	httpClient *http.Client
	logger     *slog.Logger
	seeds      []game.Location
}

type Option func(*Pool)

func WithLogger(logger *slog.Logger) Option {
	return func(p *Pool) {
		p.logger = logger
	}
}

func WithEndpoint(endpoint string) Option {
	return func(p *Pool) {
		p.endpoint = endpoint
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(p *Pool) {
		p.httpClient = client
	}
}

func New(apiKey string, opts ...Option) *Pool {
	p := &Pool{
		apiKey:     apiKey,
		endpoint:   defaultEndpoint,
		radius:     defaultSearchRadius,
		httpClient: &http.Client{Timeout: 4 * time.Second},
		logger:     slog.Default(),
		seeds:      CuratedLocations(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func (p *Pool) Pick(n int) []game.Location {
	if n <= 0 {
		return nil
	}

	// In offline simulation mode (no Google Maps API key), pick from curated seeds deterministically
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

	// Attempt on-demand discovery using Street View Metadata API across unique seed anchors
	for i := 0; len(out) < n && i < total*3; i++ {
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
		// Fallback to seed directly if API lookup fails or panorama was already seen
		if _, exists := seen[seed.ID]; !exists {
			seen[seed.ID] = struct{}{}
			out = append(out, seed)
		}
	}

	return out
}

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

	// If requested more than available seeds, repeat with random picks
	for len(out) < n {
		out = append(out, p.seeds[mrand.IntN(total)])
	}

	return out
}

func (p *Pool) Picker() func(n int) []game.Location {
	return func(n int) []game.Location {
		return p.Pick(n)
	}
}

func (p *Pool) MapName() string {
	return "curated_world"
}

func (p *Pool) Close() {
	// No background workers to close
}

func (p *Pool) discoverOne(coord game.LatLng) (game.Location, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	queryCoord := applyJitter(coord, defaultJitterKm)

	reqURL := fmt.Sprintf("%s?location=%.6f,%.6f&radius=%d&key=%s",
		p.endpoint, queryCoord.Lat, queryCoord.Lng, p.radius, url.QueryEscape(p.apiKey))

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

// applyJitter offsets a coordinate by up to maxKm in a random direction to discover
// diverse street panoramas throughout an anchor landmark's broader metropolitan area.
func applyJitter(coord game.LatLng, maxKm float64) game.LatLng {
	if maxKm <= 0 {
		return coord
	}

	// 1 degree latitude is approximately 111.0 km
	latDelta := (mrand.Float64()*2 - 1) * (maxKm / 111.0)

	// Scale longitude delta by cosine of latitude to account for meridian convergence
	rad := coord.Lat * math.Pi / 180.0
	cosLat := math.Cos(rad)
	if math.Abs(cosLat) < 0.01 {
		cosLat = 0.01 // safeguard near poles
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

type metadataResponse struct {
	Status   string `json:"status"`
	PanoID   string `json:"pano_id"`
	Location struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
}
