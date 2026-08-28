package location

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"geoduel/internal/game"
)

func TestPointInPolygonRayCasting(t *testing.T) {
	exterior := Ring{
		{Lat: 0, Lng: 0},
		{Lat: 0, Lng: 10},
		{Lat: 10, Lng: 10},
		{Lat: 10, Lng: 0},
		{Lat: 0, Lng: 0},
	}

	poly := Polygon{
		Name:     "Square",
		Exterior: exterior,
		MinLat:   0,
		MaxLat:   10,
		MinLng:   0,
		MaxLng:   10,
	}

	// Inside center
	if !poly.Contains(game.LatLng{Lat: 5, Lng: 5}) {
		t.Error("point (5,5) should be inside polygon")
	}

	// Outside
	if poly.Contains(game.LatLng{Lat: 15, Lng: 5}) {
		t.Error("point (15,5) should be outside polygon")
	}
	if poly.Contains(game.LatLng{Lat: -2, Lng: 5}) {
		t.Error("point (-2,5) should be outside polygon")
	}
}

func TestPolygonWithHole(t *testing.T) {
	poly := Polygon{
		Name: "Donut",
		Exterior: Ring{
			{Lat: 0, Lng: 0},
			{Lat: 0, Lng: 10},
			{Lat: 10, Lng: 10},
			{Lat: 10, Lng: 0},
			{Lat: 0, Lng: 0},
		},
		Holes: []Ring{
			{
				{Lat: 3, Lng: 3},
				{Lat: 3, Lng: 7},
				{Lat: 7, Lng: 7},
				{Lat: 7, Lng: 3},
				{Lat: 3, Lng: 3},
			},
		},
		MinLat: 0,
		MaxLat: 10,
		MinLng: 0,
		MaxLng: 10,
	}

	if !poly.Contains(game.LatLng{Lat: 1, Lng: 1}) {
		t.Error("point (1,1) should be inside donut")
	}

	if poly.Contains(game.LatLng{Lat: 5, Lng: 5}) {
		t.Error("point (5,5) inside hole should be excluded")
	}
}

func TestGeoJSONParsing(t *testing.T) {
	raw := `{
		"type": "FeatureCollection",
		"features": [
			{
				"type": "Feature",
				"properties": { "name": "Test Zone" },
				"geometry": {
					"type": "Polygon",
					"coordinates": [
						[[0.0, 0.0], [5.0, 0.0], [5.0, 5.0], [0.0, 5.0], [0.0, 0.0]]
					]
				}
			},
			{
				"type": "Feature",
				"properties": { "name": "Point Hub" },
				"geometry": {
					"type": "Point",
					"coordinates": [10.0, 20.0]
				}
			}
		]
	}`

	m, err := ParseGeoJSONBytes([]byte(raw))
	if err != nil {
		t.Fatalf("parse geojson: %v", err)
	}

	if len(m.Polygons) != 2 {
		t.Fatalf("expected 2 polygons, got %d", len(m.Polygons))
	}

	coord, name := m.Sample(nil)
	if !coord.Valid() {
		t.Fatalf("invalid sampled coordinate: %+v", coord)
	}
	if name == "" {
		t.Error("expected non-empty region name")
	}
}

func TestProceduralCoordinateFallback(t *testing.T) {
	for i := 0; i < 50; i++ {
		coord, region := ProceduralCoordinate(nil)
		if !coord.Valid() {
			t.Fatalf("invalid procedural coordinate: %+v", coord)
		}
		if region != "Procedural Global" {
			t.Errorf("unexpected region: %s", region)
		}
	}
}

func TestLoadMapFile(t *testing.T) {
	// Empty path should return nil, nil
	m, err := LoadMap("")
	if err != nil || m != nil {
		t.Errorf("LoadMap(\"\") = (%v, %v), want (nil, nil)", m, err)
	}

	// Temporary Map File
	tmpDir := t.TempDir()
	mapPath := filepath.Join(tmpDir, "custom.geojson")
	sampleJSON := `{
		"type": "FeatureCollection",
		"features": [
			{
				"type": "Feature",
				"properties": { "name": "Custom Region" },
				"geometry": {
					"type": "Polygon",
					"coordinates": [[[10.0, 10.0], [20.0, 10.0], [20.0, 20.0], [10.0, 20.0], [10.0, 10.0]]]
				}
			}
		]
	}`
	if err := os.WriteFile(mapPath, []byte(sampleJSON), 0644); err != nil {
		t.Fatalf("write temp map: %v", err)
	}

	loaded, err := LoadMap(mapPath)
	if err != nil {
		t.Fatalf("load custom map file: %v", err)
	}
	if loaded.Name != mapPath {
		t.Errorf("map name = %q, want %q", loaded.Name, mapPath)
	}
}

func TestSimulatedPoolOffline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := New(ctx, "", WithBufferSize(20))
	defer pool.Close()

	picked := pool.Pick(5)
	if len(picked) != 5 {
		t.Fatalf("picked %d locations, want 5", len(picked))
	}

	seen := make(map[string]struct{})
	for _, loc := range picked {
		if _, dup := seen[loc.ID]; dup {
			t.Errorf("duplicate location ID returned: %s", loc.ID)
		}
		seen[loc.ID] = struct{}{}
		if !loc.LatLng().Valid() {
			t.Errorf("invalid coordinate: %+v", loc.LatLng())
		}
		if loc.PanoID == "" {
			t.Error("pano_id should not be empty")
		}
	}

	if pool.MapName() != "procedural" {
		t.Errorf("expected map name 'procedural', got %q", pool.MapName())
	}
}

func TestDynamicGoogleMetadataAPIWithMockServer(t *testing.T) {
	var callCount atomic.Int64

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if call%3 == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ZERO_RESULTS",
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "OK",
			"pano_id": "google_pano_" + r.URL.Query().Get("location"),
			"location": map[string]float64{
				"lat": 48.8566,
				"lng": 2.3522,
			},
			"copyright": "© Google",
		})
	}))
	defer mockServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := New(ctx, "test-api-key",
		WithEndpoint(mockServer.URL),
		WithBufferSize(10),
	)
	defer pool.Close()

	picked := pool.Pick(3)
	if len(picked) != 3 {
		t.Fatalf("picked %d locations, want 3", len(picked))
	}

	if callCount.Load() < 3 {
		t.Errorf("expected at least 3 API calls, got %d", callCount.Load())
	}

	for _, loc := range picked {
		if loc.Lat != 48.8566 || loc.Lng != 2.3522 {
			t.Errorf("unexpected location coords: %+v", loc)
		}
		if loc.PanoID == "" {
			t.Error("pano_id should not be empty")
		}
	}
}

func TestPickEdgeCases(t *testing.T) {
	ctx := t.Context()

	pool := New(ctx, "")
	defer pool.Close()

	if got := pool.Pick(0); got != nil {
		t.Errorf("Pick(0) = %+v, want nil", got)
	}
	if got := pool.Pick(-5); got != nil {
		t.Errorf("Pick(-5) = %+v, want nil", got)
	}
}

func TestPickerClosure(t *testing.T) {
	ctx := t.Context()

	pool := New(ctx, "")
	defer pool.Close()

	picker := pool.Picker(nil)
	locs := picker(4)
	if len(locs) != 4 {
		t.Fatalf("picker returned %d locations, want 4", len(locs))
	}
}
