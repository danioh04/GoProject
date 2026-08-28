package location

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"os"

	"geoduel/internal/game"
)

// GeoJSON RFC 7946 Structures
type FeatureCollection struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

type Feature struct {
	Type     string   `json:"type"`
	Geometry Geometry `json:"geometry"`
}

type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

type Ring []game.LatLng

type Polygon struct {
	Exterior Ring
	Holes    []Ring
	MinLat   float64
	MaxLat   float64
	MinLng   float64
	MaxLng   float64
}

// Map holds parsed geographic boundary polygons and samplers.
type Map struct {
	Name     string
	Polygons []Polygon
}

// LoadMap reads a GeoJSON map from a file path. Returns nil, nil if path is empty.
func LoadMap(path string) (*Map, error) {
	if path == "" {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open map file %q: %w", path, err)
	}
	defer f.Close()

	m, err := ParseGeoJSON(f)
	if err != nil {
		return nil, fmt.Errorf("parse map file %q: %w", path, err)
	}

	m.Name = path
	return m, nil
}

// ParseGeoJSONBytes parses GeoJSON byte content.
func ParseGeoJSONBytes(data []byte) (*Map, error) {
	var fc FeatureCollection
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("unmarshal geojson: %w", err)
	}
	return buildMapFromCollection(fc)
}

// ParseGeoJSON parses GeoJSON from an io.Reader.
func ParseGeoJSON(r io.Reader) (*Map, error) {
	var fc FeatureCollection
	if err := json.NewDecoder(r).Decode(&fc); err != nil {
		return nil, fmt.Errorf("decode geojson: %w", err)
	}
	return buildMapFromCollection(fc)
}

func buildMapFromCollection(fc FeatureCollection) (*Map, error) {
	out := &Map{Polygons: make([]Polygon, 0, len(fc.Features))}

	for _, feat := range fc.Features {
		switch feat.Geometry.Type {
		case "Polygon":
			var raw [][][]float64
			if err := json.Unmarshal(feat.Geometry.Coordinates, &raw); err != nil {
				continue
			}
			poly, ok := parsePolygon(raw)
			if ok {
				out.Polygons = append(out.Polygons, poly)
			}

		case "MultiPolygon":
			var raw [][][][]float64
			if err := json.Unmarshal(feat.Geometry.Coordinates, &raw); err != nil {
				continue
			}
			for _, polyRaw := range raw {
				poly, ok := parsePolygon(polyRaw)
				if ok {
					out.Polygons = append(out.Polygons, poly)
				}
			}

		case "Point":
			var raw []float64
			if err := json.Unmarshal(feat.Geometry.Coordinates, &raw); err != nil || len(raw) < 2 {
				continue
			}
			// Treat Point as a micro-polygon bounding box around coordinate
			lng, lat := raw[0], raw[1]
			ring := Ring{
				{Lat: lat - 0.05, Lng: lng - 0.05},
				{Lat: lat + 0.05, Lng: lng - 0.05},
				{Lat: lat + 0.05, Lng: lng + 0.05},
				{Lat: lat - 0.05, Lng: lng + 0.05},
				{Lat: lat - 0.05, Lng: lng - 0.05},
			}
			poly, ok := parsePolygon([][][]float64{ringToFloats(ring)})
			if ok {
				out.Polygons = append(out.Polygons, poly)
			}
		}
	}

	if len(out.Polygons) == 0 {
		return nil, errors.New("no valid polygons found in GeoJSON")
	}

	return out, nil
}

func parsePolygon(ringsRaw [][][]float64) (Polygon, bool) {
	if len(ringsRaw) == 0 || len(ringsRaw[0]) < 3 {
		return Polygon{}, false
	}

	exterior := parseRing(ringsRaw[0])
	if len(exterior) < 3 {
		return Polygon{}, false
	}

	minLat, maxLat := 90.0, -90.0
	minLng, maxLng := 180.0, -180.0

	for _, pt := range exterior {
		if pt.Lat < minLat {
			minLat = pt.Lat
		}
		if pt.Lat > maxLat {
			maxLat = pt.Lat
		}
		if pt.Lng < minLng {
			minLng = pt.Lng
		}
		if pt.Lng > maxLng {
			maxLng = pt.Lng
		}
	}

	holes := make([]Ring, 0, len(ringsRaw)-1)
	for i := 1; i < len(ringsRaw); i++ {
		h := parseRing(ringsRaw[i])
		if len(h) >= 3 {
			holes = append(holes, h)
		}
	}

	return Polygon{
		Exterior: exterior,
		Holes:    holes,
		MinLat:   minLat,
		MaxLat:   maxLat,
		MinLng:   minLng,
		MaxLng:   maxLng,
	}, true
}

func parseRing(coords [][]float64) Ring {
	ring := make(Ring, 0, len(coords))
	for _, c := range coords {
		if len(c) >= 2 {
			// GeoJSON coordinate order is [longitude, latitude]
			lng, lat := c[0], c[1]
			ring = append(ring, game.LatLng{Lat: lat, Lng: lng})
		}
	}
	return ring
}

func ringToFloats(ring Ring) [][]float64 {
	out := make([][]float64, len(ring))
	for i, pt := range ring {
		out[i] = []float64{pt.Lng, pt.Lat}
	}
	return out
}

// Sample generates a random valid coordinate within the map's boundary polygons using Ray-Casting PIP.
func (m *Map) Sample(rnd *mrand.Rand) game.LatLng {
	if len(m.Polygons) == 0 {
		return ProceduralCoordinate(rnd)
	}

	var poly Polygon
	if rnd != nil {
		poly = m.Polygons[rnd.IntN(len(m.Polygons))]
	} else {
		poly = m.Polygons[mrand.IntN(len(m.Polygons))]
	}

	latRange := poly.MaxLat - poly.MinLat
	lngRange := poly.MaxLng - poly.MinLng

	// Ray-casting rejection sampling
	for attempts := 0; attempts < 30; attempts++ {
		var rLat, rLng float64
		if rnd != nil {
			rLat = poly.MinLat + rnd.Float64()*latRange
			rLng = poly.MinLng + rnd.Float64()*lngRange
		} else {
			rLat = poly.MinLat + mrand.Float64()*latRange
			rLng = poly.MinLng + mrand.Float64()*lngRange
		}

		candidate := game.LatLng{Lat: rLat, Lng: rLng}
		if poly.Contains(candidate) {
			return candidate
		}
	}

	// Fallback to polygon centroid if rejection sampling timed out
	return poly.Centroid()
}

// ProceduralCoordinate generates a completely dynamic, non-hardcoded global coordinate.
func ProceduralCoordinate(rnd *mrand.Rand) game.LatLng {
	// Sample across inhabited global land latitudes (-50 to +65) and longitudes (-180 to +180)
	var lat, lng float64
	if rnd != nil {
		lat = -50.0 + rnd.Float64()*115.0
		lng = -180.0 + rnd.Float64()*360.0
	} else {
		lat = -50.0 + mrand.Float64()*115.0
		lng = -180.0 + mrand.Float64()*360.0
	}

	return game.LatLng{Lat: lat, Lng: lng}
}

// Contains checks if point is inside exterior ring and outside all interior holes (Ray-Casting Algorithm).
func (p *Polygon) Contains(pt game.LatLng) bool {
	if pt.Lat < p.MinLat || pt.Lat > p.MaxLat || pt.Lng < p.MinLng || pt.Lng > p.MaxLng {
		return false
	}

	if !pointInRing(pt, p.Exterior) {
		return false
	}

	for _, hole := range p.Holes {
		if pointInRing(pt, hole) {
			return false
		}
	}

	return true
}

// Centroid returns the bounding center of the polygon.
func (p *Polygon) Centroid() game.LatLng {
	return game.LatLng{
		Lat: (p.MinLat + p.MaxLat) / 2.0,
		Lng: (p.MinLng + p.MaxLng) / 2.0,
	}
}

// pointInRing implements standard ray-casting Point-in-Polygon (PIP) testing.
func pointInRing(pt game.LatLng, ring Ring) bool {
	inside := false
	n := len(ring)
	if n < 3 {
		return false
	}

	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := ring[i].Lng, ring[i].Lat
		xj, yj := ring[j].Lng, ring[j].Lat

		intersect := ((yi > pt.Lat) != (yj > pt.Lat)) &&
			(pt.Lng < (xj-xi)*(pt.Lat-yi)/(yj-yi)+xi)

		if intersect {
			inside = !inside
		}
		j = i
	}

	return inside
}
