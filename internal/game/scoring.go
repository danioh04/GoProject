package game

import "math"

const (
	earthRadiusM  = 6371008.8
	mapSizeKm     = 14916.862
	decayConstant = 10.0
)

// Calculates points earned for a guess using an exponential decay curve
func Score(guess, target LatLng, maxScore int) int {
	if maxScore <= 0 {
		maxScore = DefaultConfig().MaxScore
	}

	dKm := HaversineMeters(guess, target) / 1000
	score := float64(maxScore) * math.Exp(-decayConstant*dKm/mapSizeKm)
	return int(math.Round(score))
}

// Computes the great-circle distance between two points
func HaversineMeters(a, b LatLng) float64 {
	lat1, lat2 := radians(a.Lat), radians(b.Lat)
	dLat, dLng := radians(b.Lat-a.Lat), radians(b.Lng-a.Lng)

	sinDLat, sinDLng := math.Sin(dLat/2), math.Sin(dLng/2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLng*sinDLng
	clamped := math.Max(0, math.Min(1, h))

	return 2 * earthRadiusM * math.Asin(math.Sqrt(clamped))
}

// Converts degrees to radians
func radians(deg float64) float64 {
	return deg * math.Pi / 180
}
