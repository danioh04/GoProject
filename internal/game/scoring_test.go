package game

import (
	"math"
	"testing"
)

func TestHaversineKnownDistances(t *testing.T) {
	cases := []struct {
		name  string
		a, b  LatLng
		wantM float64
		tolM  float64
	}{
		{"paris-london", LatLng{48.8566, 2.3522}, LatLng{51.5074, -0.1278}, 343556, 2000},
		{"nyc-la", LatLng{40.7128, -74.006}, LatLng{34.0522, -118.2437}, 3935746, 25000},
		{"same-point", LatLng{-33.86, 151.21}, LatLng{-33.86, 151.21}, 0, 0.001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := haversineMeters(tc.a, tc.b)
			if math.Abs(got-tc.wantM) > tc.tolM {
				t.Errorf("distance = %.0f m, want %.0f ±%.0f m", got, tc.wantM, tc.tolM)
			}
		})
	}
}

func TestScoreExactGuessIsMaxScore(t *testing.T) {
	target := LatLng{48.8566, 2.3522}
	if got := Score(target, target, 5000); got != 5000 {
		t.Errorf("exact guess score = %d, want 5000", got)
	}
}

func TestScoreDecreasesMonotonicallyWithDistance(t *testing.T) {
	target := LatLng{0, 0}
	prev := math.MaxInt
	for _, km := range []float64{0.001, 100, 500, 1000, 3000, 8000} {
		lng := km / 111.195
		guess := LatLng{Lat: 0, Lng: lng}
		got := Score(guess, target, 5000)
		if got >= prev {
			t.Errorf("score at %.0f km = %d, want < previous %d", km, got, prev)
		}
		prev = got
	}
}

func TestScoreFarGuessNearZero(t *testing.T) {
	got := Score(LatLng{-45, 135}, LatLng{45, -45}, 5000)
	if got < 0 || got > 50 {
		t.Errorf("near-antipodal score = %d, want within [0,50]", got)
	}
}

func TestLatLngValidation(t *testing.T) {
	valid := []LatLng{
		{0, 0}, {90, 180}, {-90, -180}, {45.5, -122.6},
	}
	for _, p := range valid {
		if !p.Valid() {
			t.Errorf("%+v should be valid", p)
		}
	}
	invalid := []LatLng{
		{91, 0}, {-90.0001, 0}, {0, 181}, {0, -180.0001},
		{math.NaN(), 0}, {0, math.Inf(1)},
	}
	for _, p := range invalid {
		if p.Valid() {
			t.Errorf("%+v should be invalid", p)
		}
	}
}

func BenchmarkHaversine(b *testing.B) {
	p1 := LatLng{48.8566, 2.3522}
	p2 := LatLng{35.6762, 139.6503}
	b.ReportAllocs()

	for b.Loop() {
		_ = haversineMeters(p1, p2)
	}
}

func BenchmarkScore(b *testing.B) {
	guess := LatLng{48.8566, 2.3522}
	target := LatLng{48.8500, 2.3500}
	b.ReportAllocs()

	for b.Loop() {
		_ = Score(guess, target, 5000)
	}
}
