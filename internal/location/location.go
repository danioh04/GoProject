package location

import (
	"context"
	"encoding/json"
	"fmt"
	"geoduel/internal/game"
	"geoduel/internal/randutil"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultEndpoint     = "https://maps.googleapis.com/maps/api/streetview/metadata"
	defaultSearchRadius = 50000
	defaultBufferSize   = 50
	defaultWorkers      = 3
)

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

type Option func(*Pool)

func WithLogger(logger *slog.Logger) Option {
	return func(p *Pool) {
		p.logger = logger
	}
}

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
		default:
			goto waitForBuffer
		}
	}
	return out

waitForBuffer:
	if p.apiKey != "" {
		timer := time.NewTimer(300 * time.Millisecond)
		defer timer.Stop()

		for len(out) < n {
			select {
			case loc := <-p.buffer:
				if _, exists := seen[loc.ID]; !exists && loc.Valid() {
					seen[loc.ID] = struct{}{}
					out = append(out, loc)
				}
			case <-timer.C:
				loc, err := p.discoverOne(p.ctx)
				if err == nil && loc.Valid() {
					if _, exists := seen[loc.ID]; !exists {
						seen[loc.ID] = struct{}{}
						out = append(out, loc)
					}
				} else if p.logger != nil {
					p.logger.Warn("direct street view discovery failed", "error", err)
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(300 * time.Millisecond)
		}
		return out
	}

	for len(out) < n {
		var coord game.LatLng
		if p.geoMap != nil {
			coord = p.geoMap.Sample()
		} else {
			coord = ProceduralCoordinate()
		}
		fallback := simulatedLocation(coord)
		if _, exists := seen[fallback.ID]; !exists {
			seen[fallback.ID] = struct{}{}
			out = append(out, fallback)
		}
	}

	return out
}

func (p *Pool) Picker() func(n int) []game.Location {
	return func(n int) []game.Location {
		return p.Pick(n)
	}
}

func (p *Pool) MapName() string {
	if p.geoMap != nil {
		return p.geoMap.Name
	}
	return "procedural"
}

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
	for range count {
		p.wg.Add(1)
		go func() {
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
		coord = p.geoMap.Sample()
	} else {
		coord = ProceduralCoordinate()
	}

	if !coord.Valid() {
		return game.Location{}, fmt.Errorf("invalid coordinate sampled")
	}

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
	panoID := "sim_" + randutil.Hex(8)
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
}
