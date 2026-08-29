package location

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"geoduel/internal/game"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultEndpoint     = "https://maps.googleapis.com/maps/api/streetview/metadata"
	defaultSearchRadius = 50000 // 50km radius for street view coverage
	defaultBufferSize   = 50
	defaultWorkers      = 3
)

// Pool maintains a pre-warmed background buffer of live discovered Google Street View locations.
type Pool struct {
	apiKey     string
	endpoint   string
	radius     int
	httpClient *http.Client
	logger     *slog.Logger
	geoMap     *Map

	buffer   chan game.Location
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	isClosed bool
	mu       sync.Mutex
}

// Option configures the location Pool.
type Option func(*Pool)

// WithEndpoint overrides the Google Street View API endpoint (useful for mock tests).
func WithEndpoint(endpoint string) Option {
	return func(p *Pool) {
		p.endpoint = endpoint
	}
}

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Pool) {
		p.httpClient = client
	}
}

// WithBufferSize sets the pre-warmed location buffer size.
func WithBufferSize(size int) Option {
	return func(p *Pool) {
		if size > 0 {
			p.buffer = make(chan game.Location, size)
		}
	}
}

// WithLogger sets the logger for location discovery.
func WithLogger(logger *slog.Logger) Option {
	return func(p *Pool) {
		p.logger = logger
	}
}

// WithMap sets the GeoJSON map explicitly.
func WithMap(m *Map) Option {
	return func(p *Pool) {
		p.geoMap = m
	}
}

// WithMapFile loads the GeoJSON map from a file path.
func WithMapFile(path string) Option {
	return func(p *Pool) {
		if path != "" {
			m, err := LoadMap(path)
			if err != nil {
				if p.logger != nil {
					p.logger.Error("failed to load map file", "path", path, "error", err)
				}
			} else {
				p.geoMap = m
			}
		}
	}
}

// New creates and starts a dynamic Google Street View location discovery pool.
func New(ctx context.Context, apiKey string, opts ...Option) *Pool {
	cctx, cancel := context.WithCancel(ctx)
	p := &Pool{
		apiKey:     apiKey,
		endpoint:   defaultEndpoint,
		radius:     defaultSearchRadius,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     slog.Default(),
		buffer:     make(chan game.Location, defaultBufferSize),
		ctx:        cctx,
		cancel:     cancel,
	}

	for _, opt := range opts {
		opt(p)
	}

	p.startWorkers(defaultWorkers)
	return p
}

// Pick returns n distinct locations from the pre-warmed discovery buffer.
func (p *Pool) Pick(n int) []game.Location {
	if n <= 0 {
		return nil
	}

	out := make([]game.Location, 0, n)
	seen := make(map[string]struct{}, n)

	for len(out) < n {
		select {
		case loc := <-p.buffer:
			if _, exists := seen[loc.ID]; !exists && loc.Valid() {
				seen[loc.ID] = struct{}{}
				out = append(out, loc)
			}
		case <-time.After(150 * time.Millisecond):
			// If buffer is drained, sample directly
			var coord game.LatLng
			if p.geoMap != nil {
				coord = p.geoMap.Sample(nil)
			} else {
				coord = ProceduralCoordinate(nil)
			}
			fallback := simulatedLocation(coord)
			if _, exists := seen[fallback.ID]; !exists {
				seen[fallback.ID] = struct{}{}
				out = append(out, fallback)
			}
		}
	}

	return out
}

// Picker returns a picker closure compatible with game.Engine.
func (p *Pool) Picker(_ *mrand.Rand) func(n int) []game.Location {
	return func(n int) []game.Location {
		return p.Pick(n)
	}
}

// BufferLen returns current count of pre-warmed locations available.
func (p *Pool) BufferLen() int {
	return len(p.buffer)
}

// MapName returns the active map name.
func (p *Pool) MapName() string {
	if p.geoMap != nil {
		return p.geoMap.Name
	}
	return "procedural"
}

// Close gracefully terminates background discovery workers.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.isClosed {
		p.mu.Unlock()
		return
	}
	p.isClosed = true
	p.mu.Unlock()

	p.cancel()
	p.wg.Wait()
}

func (p *Pool) startWorkers(count int) {
	for i := range count {
		p.wg.Add(1)
		go func() {
			var _ int = i
			p.workerLoop()
		}()
	}
}

func (p *Pool) workerLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		loc, err := p.discoverOne(p.ctx)
		if err != nil {
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}

		select {
		case p.buffer <- loc:
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Pool) discoverOne(ctx context.Context) (game.Location, error) {
	var coord game.LatLng
	if p.geoMap != nil {
		coord = p.geoMap.Sample(nil)
	} else {
		coord = ProceduralCoordinate(nil)
	}

	if !coord.Valid() {
		return game.Location{}, fmt.Errorf("invalid coordinate sampled")
	}

	// If no API key is provided, run in simulated offline discovery mode
	if p.apiKey == "" {
		return simulatedLocation(coord), nil
	}

	reqURL := fmt.Sprintf("%s?location=%.6f,%.6f&radius=%d&key=%s",
		p.endpoint, coord.Lat, coord.Lng, p.radius, url.QueryEscape(p.apiKey))

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

func simulatedLocation(coord game.LatLng) game.Location {
	var b [8]byte
	_, _ = rand.Read(b[:])
	panoID := "sim_" + hex.EncodeToString(b[:])

	return game.Location{
		ID:     panoID,
		PanoID: panoID,
		LatLng: coord,
	}
}

type metadataResponse struct {
	Status   string `json:"status"`
	PanoID   string `json:"pano_id"`
	Location struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"location"`
	Copyright string `json:"copyright"`
	Date      string `json:"date"`
}
