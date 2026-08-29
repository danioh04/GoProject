package location

import (
	"encoding/json"
	"errors"
	"fmt"
	"geoduel/internal/game"
	"io"
	mrand "math/rand/v2"
	"os"
	"slices"
)

type Map struct {
	Name            string
	Points          []game.LatLng
	Polygons        []Polygon
	CumulativeAreas []float64
	TotalArea       float64
}

type Ring []game.LatLng

type Polygon struct {
	Exterior Ring
	Holes    []Ring
	MinLat   float64
	MaxLat   float64
	MinLng   float64
	MaxLng   float64
	Area     float64
}

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

func ParseGeoJSON(r io.Reader) (*Map, error) {
	var fc FeatureCollection
	if err := json.NewDecoder(r).Decode(&fc); err != nil {
		return nil, fmt.Errorf("decode geojson: %w", err)
	}
	return buildMapFromCollection(fc)
}

func (m *Map) Sample() game.LatLng {
	hasPoints := len(m.Points) > 0
	hasPolys := len(m.Polygons) > 0

	if !hasPoints && !hasPolys {
		return ProceduralCoordinate()
	}

	if hasPoints && !hasPolys {
		return m.Points[mrand.IntN(len(m.Points))]
	}

	if hasPoints && hasPolys {
		if mrand.Float64() < 0.5 {
			return m.Points[mrand.IntN(len(m.Points))]
		}
	}

	poly := m.selectWeightedPolygon()

	latRange := poly.MaxLat - poly.MinLat
	lngRange := poly.MaxLng - poly.MinLng

	for range 30 {
		rLat := poly.MinLat + mrand.Float64()*latRange
		rLng := poly.MinLng + mrand.Float64()*lngRange

		candidate := game.LatLng{Lat: rLat, Lng: rLng}
		if poly.Contains(candidate) {
			return candidate
		}
	}

	return poly.Centroid()
}

func (m *Map) selectWeightedPolygon() Polygon {
	if len(m.Polygons) == 1 || m.TotalArea <= 0 {
		return m.Polygons[0]
	}

	target := mrand.Float64() * m.TotalArea
	idx, _ := slices.BinarySearch(m.CumulativeAreas, target)
	if idx >= len(m.Polygons) {
		idx = len(m.Polygons) - 1
	}
	return m.Polygons[idx]
}

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

func (p *Polygon) Centroid() game.LatLng {
	return game.LatLng{
		Lat: (p.MinLat + p.MaxLat) / 2.0,
		Lng: (p.MinLng + p.MaxLng) / 2.0,
	}
}

func ProceduralCoordinate() game.LatLng {
	return GlobalAnchorCoordinate()
}

func buildMapFromCollection(fc FeatureCollection) (*Map, error) {
	out := &Map{
		Points:   make([]game.LatLng, 0),
		Polygons: make([]Polygon, 0, len(fc.Features)),
	}

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
			lng, lat := raw[0], raw[1]
			pt := game.LatLng{Lat: lat, Lng: lng}
			if pt.Valid() {
				out.Points = append(out.Points, pt)
			}

		case "MultiPoint":
			var raw [][]float64
			if err := json.Unmarshal(feat.Geometry.Coordinates, &raw); err != nil {
				continue
			}
			for _, c := range raw {
				if len(c) >= 2 {
					pt := game.LatLng{Lat: c[1], Lng: c[0]}
					if pt.Valid() {
						out.Points = append(out.Points, pt)
					}
				}
			}
		}
	}

	if len(out.Polygons) == 0 && len(out.Points) == 0 {
		return nil, errors.New("no valid polygons or points found in GeoJSON")
	}

	if len(out.Polygons) > 0 {
		out.CumulativeAreas = make([]float64, len(out.Polygons))
		var running float64
		for i := range out.Polygons {
			p := &out.Polygons[i]
			p.Area = (p.MaxLat - p.MinLat) * (p.MaxLng - p.MinLng)
			if p.Area <= 0 {
				p.Area = 0.001
			}
			running += p.Area
			out.CumulativeAreas[i] = running
		}
		out.TotalArea = running
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
			lng, lat := c[0], c[1]
			ring = append(ring, game.LatLng{Lat: lat, Lng: lng})
		}
	}
	return ring
}

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
