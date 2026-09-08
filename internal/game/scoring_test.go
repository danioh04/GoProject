package game

import (
	"math"
	"testing"
)

func TestScore_ZeroDistance(t *testing.T) {
	pt := LatLng{Lat: 40.7128, Lng: -74.0060} // New York
	score := Score(pt, pt, 5000)
	if score != 5000 {
		t.Fatalf("expected perfect score 5000 for zero distance, got %d", score)
	}
}

func TestScore_AntipodalPoints(t *testing.T) {
	a := LatLng{Lat: 45.0, Lng: 0.0}
	b := LatLng{Lat: -45.0, Lng: 180.0}
	score := Score(a, b, 5000)
	if score != 0 {
		t.Fatalf("expected 0 for antipodal points, got %d", score)
	}
}

func TestScore_KnownDistanceLondonToParis(t *testing.T) {
	london := LatLng{Lat: 51.5074, Lng: -0.1278}
	paris := LatLng{Lat: 48.8566, Lng: 2.3522}

	dist := haversineMeters(london, paris) / 1000.0
	// London to Paris is ~343 km
	if math.Abs(dist-343.5) > 10.0 {
		t.Fatalf("expected ~343km between London and Paris, got %.2f km", dist)
	}

	score := Score(london, paris, 5000)
	// For ~344 km, score should be ~5000 * exp(-10 * 344 / 14917) ≈ 3971
	if score < 3900 || score > 4100 {
		t.Fatalf("expected score around 3970, got %d", score)
	}
}

func BenchmarkScore(b *testing.B) {
	p1 := LatLng{Lat: 37.7749, Lng: -122.4194}
	p2 := LatLng{Lat: 34.0522, Lng: -118.2437}
	b.ReportAllocs()

	for b.Loop() {
		_ = Score(p1, p2, 5000)
	}
}

func BenchmarkHaversineMeters(b *testing.B) {
	p1 := LatLng{Lat: 40.7128, Lng: -74.0060}
	p2 := LatLng{Lat: 51.5074, Lng: -0.1278}
	b.ReportAllocs()

	for b.Loop() {
		_ = haversineMeters(p1, p2)
	}
}
