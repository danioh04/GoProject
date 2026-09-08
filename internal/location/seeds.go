package location

import (
	mrand "math/rand/v2"
	"prism/internal/game"
)

type Seed struct {
	ID     string
	PanoID string
	Lat    float64
	Lng    float64
	Label  string
}

func CuratedLocations() []game.Location {
	out := make([]game.Location, len(globalSeeds))
	for i, s := range globalSeeds {
		out[i] = game.Location{
			ID:     s.ID,
			PanoID: s.PanoID,
			LatLng: game.LatLng{Lat: s.Lat, Lng: s.Lng},
		}
	}
	return out
}

func RandomSeed() game.Location {
	s := globalSeeds[mrand.IntN(len(globalSeeds))]
	return game.Location{
		ID:     s.ID,
		PanoID: s.PanoID,
		LatLng: game.LatLng{Lat: s.Lat, Lng: s.Lng},
	}
}

var globalSeeds = []Seed{
	// North America (3)
	{ID: "new_york", PanoID: "CAoSLEFGMVFpcE5VZGpmTjZ2X011LWl1S0dndjh1UG5kRWdfaEpBblB3aXlGUTdB", Lat: 40.7128, Lng: -74.0060, Label: "New York City, USA"},
	{ID: "vancouver", PanoID: "CAoSLEFGMVFpcE9uZEZ1X2Z2VlJ2TEFzTmt6aERqWHFfZVpOUDJzZ0s3V2lFc3pP", Lat: 49.2827, Lng: -123.1207, Label: "Vancouver, Canada"},
	{ID: "mexico_city", PanoID: "CAoSLEFGMVFpcE1kTXl4X3ZpTGZ0aUpqVlJ6WjVubFF0Q29aR0hfdXlTVDkyM3lZ", Lat: 19.4326, Lng: -99.1332, Label: "Mexico City, Mexico"},

	// South America (3)
	{ID: "sao_paulo", PanoID: "CAoSLEFGMVFpcE96U3h2YlZ1TGZ1T0d2aVJqVFVrWGh4dnhYVkp2Z0JfbVRvT3dv", Lat: -23.5505, Lng: -46.6333, Label: "São Paulo, Brazil"},
	{ID: "buenos_aires", PanoID: "CAoSLEFGMVFpcE9kY2t6Z2ZId21QNWt2aE5tTEJ3WnB1SndXSk9iUEFxc3BScTdz", Lat: -34.6037, Lng: -58.3816, Label: "Buenos Aires, Argentina"},
	{ID: "santiago", PanoID: "CAoSLEFGMVFpcE1mWEQ3VkpQUkZ6Q3Q4U295V2k0Vk11N2x0SE8tdWZfR0p1Q2Fm", Lat: -33.4489, Lng: -70.6693, Label: "Santiago, Chile"},

	// Europe (4)
	{ID: "london", PanoID: "CAoSLEFGMVFpcE5vY09mblF0aEZ4U3Zub3F2ZHNwNnRzWVhGOGlJOFNXZmlhTjlB", Lat: 51.5074, Lng: -0.1278, Label: "London, UK"},
	{ID: "paris", PanoID: "CAoSLEFGMVFpcE1QZkRnbE9oVUZxWGp3X1d2VUZXU012YlF5Vk00VVRzTkZ6VGl2", Lat: 48.8566, Lng: 2.3522, Label: "Paris, France"},
	{ID: "rome", PanoID: "CAoSLEFGMVFpcE53S1FzaVpXbVFSbk5mZlB4VHpVb3Z6c1BqaV9HVFZ6YkQ1UXNq", Lat: 41.9028, Lng: 12.4964, Label: "Rome, Italy"},
	{ID: "stockholm", PanoID: "CAoSLEFGMVFpcE56d3NuTFlzZE1UWHZ6blF4V09pUml1Vk55eXFYZ0N0T2tYdGNm", Lat: 59.3293, Lng: 18.0686, Label: "Stockholm, Sweden"},
	{ID: "istanbul", PanoID: "CAoSLEFGMVFpcE1lY2Z0ZlB4cjlzTEV0aFpmY1Fsc1R3Tk96SEFvY29xY0JsaG1K", Lat: 41.0082, Lng: 28.9784, Label: "Istanbul, Turkey"},

	// Asia (4)
	{ID: "dubai", PanoID: "CAoSLEFGMVFpcE1uRkZ6T0ZzS2Z3UmtzVmt2VFBxWVpqQ3I4dnhrb0xLSHc1", Lat: 25.2048, Lng: 55.2708, Label: "Dubai, UAE"},
	{ID: "mumbai", PanoID: "CAoSLEFGMVFpcE5xVFR6aXF3VmNScU93aVd0SFZsbVp5aGNjSnVqV0Zrb1lHSm5v", Lat: 19.0760, Lng: 72.8777, Label: "Mumbai, India"},
	{ID: "bangkok", PanoID: "CAoSLEFGMVFpcE5xVFR6aXF3VmNScU93aVd0SFZsbVp5aGNjSnVqV0Zrb1lHSm5w", Lat: 13.7563, Lng: 100.5018, Label: "Bangkok, Thailand"},
	{ID: "tokyo", PanoID: "CAoSLEFGMVFpcE1qVEh0VkpXck9sRk10U1Z6YkRmV09vU1FzS0d4Y1N5VG9uV05v", Lat: 35.6762, Lng: 139.6503, Label: "Tokyo, Japan"},
	{ID: "seoul", PanoID: "CAoSLEFGMVFpcE16aG55VkZ6U1J1WmtzWEZ2b2tqU2x5ZFl6R0h5YmF5S1FjOHB2", Lat: 37.5665, Lng: 126.9780, Label: "Seoul, South Korea"},

	// Africa (2)
	{ID: "cape_town", PanoID: "CAoSLEFGMVFpcE1OclJrbk1jS0VnclZ6aE10c3Z2aU9jZ1l1VkI3dEZhT2h6T1l0", Lat: -33.9249, Lng: 18.4241, Label: "Cape Town, South Africa"},
	{ID: "nairobi", PanoID: "CAoSLEFGMVFpcE5rYlI3WmtvUUVXU0pHTXNfRWptbFk4b2R5bFl4ZUpQZURpQ0l3", Lat: -1.2921, Lng: 36.8219, Label: "Nairobi, Kenya"},

	// Oceania (2)
	{ID: "sydney", PanoID: "CAoSLEFGMVFpcE9aUmx0T05mS2x0Smx1U0ZwWnhHcnF4Y0NuR0h5YmF5S1FjOHB2", Lat: -33.8688, Lng: 151.2093, Label: "Sydney, Australia"},
	{ID: "auckland", PanoID: "CAoSLEFGMVFpcE5yYlI3WmtvUUVXU0pHTXNfRWptbFk4b2R5bFl4ZUpQZURpQ0l3", Lat: -36.8485, Lng: 174.7633, Label: "Auckland, New Zealand"},
}
