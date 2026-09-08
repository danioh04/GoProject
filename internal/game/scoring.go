package game

import "math"

const (
	// earthRadiusM is the mean volumetric radius of Earth in meters (WGS 84 / IUGG standard).
	earthRadiusM = 6371008.8
	// mapSizeKM represents the maximum half-circumference / typical map scale factor in kilometers.
	mapSizeKM = 14916.862
	// decayConstant controls the rate of exponential score decay as distance increases.
	decayConstant = 10.0
)

// Score calculates points earned for a guess relative to the target using an exponential decay curve:
// Score = maxScore * exp(-decayConstant * distance_km / mapSizeKM)
func Score(guess, target LatLng, maxScore int) int {
	if maxScore <= 0 {
		maxScore = DefaultConfig().MaxScore
	}
	dKm := haversineMeters(guess, target) / 1000
	score := float64(maxScore) * math.Exp(-decayConstant*dKm/mapSizeKM)
	return int(math.Round(score))
}

// HaversineMeters computes the great-circle distance between two points on a sphere in meters.
func HaversineMeters(a, b LatLng) float64 {
	lat1, lat2 := radians(a.Lat), radians(b.Lat)
	dLat, dLng := radians(b.Lat-a.Lat), radians(b.Lng-a.Lng)

	sinDLat, sinDLng := math.Sin(dLat/2), math.Sin(dLng/2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLng*sinDLng

	clamped := math.Max(0, math.Min(1, h))
	return 2 * earthRadiusM * math.Asin(math.Sqrt(clamped))
}

func haversineMeters(a, b LatLng) float64 {
	return HaversineMeters(a, b)
}

func radians(deg float64) float64 {
	return deg * math.Pi / 180
}
