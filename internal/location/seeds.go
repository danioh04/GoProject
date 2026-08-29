package location

import (
	"geoduel/internal/game"
	mrand "math/rand/v2"
)

func GlobalAnchorCoordinate() game.LatLng {
	anchor := globalSeeds[mrand.IntN(len(globalSeeds))]

	jLat := (mrand.Float64()*2.0 - 1.0) * 0.08
	jLng := (mrand.Float64()*2.0 - 1.0) * 0.08

	cand := game.LatLng{Lat: anchor.Lat + jLat, Lng: anchor.Lng + jLng}
	if cand.Valid() {
		return cand
	}
	return anchor
}

var globalSeeds = []game.LatLng{
	{Lat: 40.7128, Lng: -74.0060},  // New York, NY
	{Lat: 34.0522, Lng: -118.2437}, // Los Angeles, CA
	{Lat: 41.8781, Lng: -87.6298},  // Chicago, IL
	{Lat: 29.7604, Lng: -95.3698},  // Houston, TX
	{Lat: 33.4484, Lng: -112.0740}, // Phoenix, AZ
	{Lat: 39.9526, Lng: -75.1652},  // Philadelphia, PA
	{Lat: 29.4241, Lng: -98.4936},  // San Antonio, TX
	{Lat: 32.7157, Lng: -117.1611}, // San Diego, CA
	{Lat: 32.7767, Lng: -96.7970},  // Dallas, TX
	{Lat: 37.7749, Lng: -122.4194}, // San Francisco, CA
	{Lat: 30.2672, Lng: -97.7431},  // Austin, TX
	{Lat: 39.7392, Lng: -104.9903}, // Denver, CO
	{Lat: 47.6062, Lng: -122.3321}, // Seattle, WA
	{Lat: 42.3601, Lng: -71.0589},  // Boston, MA
	{Lat: 25.7617, Lng: -80.1918},  // Miami, FL
	{Lat: 33.7490, Lng: -84.3880},  // Atlanta, GA
	{Lat: 36.1627, Lng: -86.7816},  // Nashville, TN
	{Lat: 45.5152, Lng: -122.6784}, // Portland, OR
	{Lat: 35.2271, Lng: -80.8431},  // Charlotte, NC
	{Lat: 38.9072, Lng: -77.0369},  // Washington, DC
	{Lat: 36.1699, Lng: -115.1398}, // Las Vegas, NV
	{Lat: 44.9778, Lng: -93.2650},  // Minneapolis, MN
	{Lat: 29.9511, Lng: -90.0715},  // New Orleans, LA
	{Lat: 35.0844, Lng: -106.6504}, // Albuquerque, NM
	{Lat: 40.7608, Lng: -111.8910}, // Salt Lake City, UT
	{Lat: 21.3069, Lng: -157.8583}, // Honolulu, HI
	{Lat: 61.2181, Lng: -149.9003}, // Anchorage, AK

	{Lat: 43.6532, Lng: -79.3832},  // Toronto, ON
	{Lat: 45.5017, Lng: -73.5673},  // Montreal, QC
	{Lat: 49.2827, Lng: -123.1207}, // Vancouver, BC
	{Lat: 51.0447, Lng: -114.0719}, // Calgary, AB
	{Lat: 53.5461, Lng: -113.4938}, // Edmonton, AB
	{Lat: 45.4215, Lng: -75.6972},  // Ottawa, ON
	{Lat: 44.6488, Lng: -63.5752},  // Halifax, NS
	{Lat: 49.8951, Lng: -97.1384},  // Winnipeg, MB
	{Lat: 46.8139, Lng: -71.2080},  // Quebec City, QC
	{Lat: 48.4284, Lng: -123.3656}, // Victoria, BC

	{Lat: 19.4326, Lng: -99.1332},  // Mexico City
	{Lat: 20.6597, Lng: -103.3496}, // Guadalajara
	{Lat: 25.6866, Lng: -100.3161}, // Monterrey
	{Lat: 21.1619, Lng: -86.8515},  // Cancún
	{Lat: 19.0414, Lng: -98.2063},  // Puebla
	{Lat: 32.5149, Lng: -117.0382}, // Tijuana
	{Lat: 20.9674, Lng: -89.5926},  // Mérida
	{Lat: 21.8853, Lng: -102.2916}, // Aguascalientes
	{Lat: 20.5888, Lng: -100.3899}, // Querétaro
	{Lat: 17.0732, Lng: -96.7266},  // Oaxaca

	{Lat: 51.5074, Lng: -0.1278},  // London, UK
	{Lat: 53.4808, Lng: -2.2426},  // Manchester, UK
	{Lat: 55.9533, Lng: -3.1883},  // Edinburgh, UK
	{Lat: 53.3498, Lng: -6.2603},  // Dublin, Ireland
	{Lat: 48.8566, Lng: 2.3522},   // Paris, France
	{Lat: 43.2965, Lng: 5.3698},   // Marseille, France
	{Lat: 45.7640, Lng: 4.8357},   // Lyon, France
	{Lat: 43.6047, Lng: 1.4442},   // Toulouse, France
	{Lat: 44.8378, Lng: -0.5792},  // Bordeaux, France
	{Lat: 50.8503, Lng: 4.3517},   // Brussels, Belgium
	{Lat: 51.2194, Lng: 4.4025},   // Antwerp, Belgium
	{Lat: 52.3676, Lng: 4.9041},   // Amsterdam, Netherlands
	{Lat: 51.9244, Lng: 4.4777},   // Rotterdam, Netherlands
	{Lat: 49.6116, Lng: 6.1319},   // Luxembourg City
	{Lat: 52.5200, Lng: 13.4050},  // Berlin, Germany
	{Lat: 48.1351, Lng: 11.5820},  // Munich, Germany
	{Lat: 53.5511, Lng: 9.9937},   // Hamburg, Germany
	{Lat: 50.1109, Lng: 8.6821},   // Frankfurt, Germany
	{Lat: 50.9375, Lng: 6.9603},   // Cologne, Germany
	{Lat: 46.9480, Lng: 7.4474},   // Bern, Switzerland
	{Lat: 47.3769, Lng: 8.5417},   // Zurich, Switzerland
	{Lat: 46.2044, Lng: 6.1432},   // Geneva, Switzerland
	{Lat: 48.2082, Lng: 16.3738},  // Vienna, Austria
	{Lat: 47.8095, Lng: 13.0550},  // Salzburg, Austria
	{Lat: 59.3293, Lng: 18.0686},  // Stockholm, Sweden
	{Lat: 57.7089, Lng: 11.9746},  // Gothenburg, Sweden
	{Lat: 59.9139, Lng: 10.7522},  // Oslo, Norway
	{Lat: 60.3913, Lng: 5.3221},   // Bergen, Norway
	{Lat: 55.6761, Lng: 12.5683},  // Copenhagen, Denmark
	{Lat: 60.1699, Lng: 24.9384},  // Helsinki, Finland
	{Lat: 64.1466, Lng: -21.9426}, // Reykjavik, Iceland

	{Lat: 40.4168, Lng: -3.7038}, // Madrid, Spain
	{Lat: 41.3879, Lng: 2.1699},  // Barcelona, Spain
	{Lat: 39.4699, Lng: -0.3763}, // Valencia, Spain
	{Lat: 37.3891, Lng: -5.9845}, // Seville, Spain
	{Lat: 38.7223, Lng: -9.1393}, // Lisbon, Portugal
	{Lat: 41.1579, Lng: -8.6291}, // Porto, Portugal
	{Lat: 41.9028, Lng: 12.4964}, // Rome, Italy
	{Lat: 45.4642, Lng: 9.1900},  // Milan, Italy
	{Lat: 40.8518, Lng: 14.2681}, // Naples, Italy
	{Lat: 43.7696, Lng: 11.2558}, // Florence, Italy
	{Lat: 45.4408, Lng: 12.3155}, // Venice, Italy
	{Lat: 38.1157, Lng: 13.3615}, // Palermo, Italy
	{Lat: 37.9838, Lng: 23.7275}, // Athens, Greece
	{Lat: 40.6401, Lng: 22.9444}, // Thessaloniki, Greece
	{Lat: 52.2297, Lng: 21.0122}, // Warsaw, Poland
	{Lat: 50.0647, Lng: 19.9450}, // Krakow, Poland
	{Lat: 51.1079, Lng: 17.0385}, // Wroclaw, Poland
	{Lat: 50.0755, Lng: 14.4378}, // Prague, Czech Republic
	{Lat: 49.1951, Lng: 16.6068}, // Brno, Czech Republic
	{Lat: 48.1486, Lng: 17.1077}, // Bratislava, Slovakia
	{Lat: 47.4979, Lng: 19.0402}, // Budapest, Hungary
	{Lat: 46.0569, Lng: 14.5058}, // Ljubljana, Slovenia
	{Lat: 45.8150, Lng: 15.9819}, // Zagreb, Croatia
	{Lat: 43.5081, Lng: 16.4402}, // Split, Croatia
	{Lat: 44.7866, Lng: 20.4489}, // Belgrade, Serbia
	{Lat: 44.4268, Lng: 26.1025}, // Bucharest, Romania
	{Lat: 46.7712, Lng: 23.6236}, // Cluj-Napoca, Romania
	{Lat: 42.6977, Lng: 23.3219}, // Sofia, Bulgaria
	{Lat: 59.4370, Lng: 24.7536}, // Tallinn, Estonia
	{Lat: 56.9496, Lng: 24.1052}, // Riga, Latvia
	{Lat: 54.6872, Lng: 25.2797}, // Vilnius, Lithuania
	{Lat: 35.8989, Lng: 14.5146}, // Valletta, Malta

	{Lat: 35.6762, Lng: 139.6503}, // Tokyo, Japan
	{Lat: 34.6937, Lng: 135.5023}, // Osaka, Japan
	{Lat: 35.0116, Lng: 135.7681}, // Kyoto, Japan
	{Lat: 43.0618, Lng: 141.3545}, // Sapporo, Japan
	{Lat: 33.5904, Lng: 130.4017}, // Fukuoka, Japan
	{Lat: 34.3853, Lng: 132.4553}, // Hiroshima, Japan
	{Lat: 35.1815, Lng: 136.9066}, // Nagoya, Japan
	{Lat: 38.2682, Lng: 140.8694}, // Sendai, Japan
	{Lat: 26.2124, Lng: 127.6809}, // Naha, Okinawa, Japan
	{Lat: 37.5665, Lng: 126.9780}, // Seoul, South Korea
	{Lat: 35.1796, Lng: 129.0756}, // Busan, South Korea
	{Lat: 37.4563, Lng: 126.7052}, // Incheon, South Korea
	{Lat: 35.8714, Lng: 128.6014}, // Daegu, South Korea
	{Lat: 33.4996, Lng: 126.5312}, // Jeju, South Korea
	{Lat: 25.0330, Lng: 121.5654}, // Taipei, Taiwan
	{Lat: 22.6273, Lng: 120.3014}, // Kaohsiung, Taiwan
	{Lat: 24.1477, Lng: 120.6736}, // Taichung, Taiwan
	{Lat: 13.7563, Lng: 100.5018}, // Bangkok, Thailand
	{Lat: 18.7883, Lng: 98.9853},  // Chiang Mai, Thailand
	{Lat: 7.8804, Lng: 98.3923},   // Phuket, Thailand
	{Lat: 3.1390, Lng: 101.6869},  // Kuala Lumpur, Malaysia
	{Lat: 5.4141, Lng: 100.3288},  // George Town, Penang, Malaysia
	{Lat: 1.3521, Lng: 103.8198},  // Singapore
	{Lat: -6.2088, Lng: 106.8456}, // Jakarta, Indonesia
	{Lat: -7.2575, Lng: 112.7521}, // Surabaya, Indonesia
	{Lat: -6.9175, Lng: 107.6191}, // Bandung, Indonesia
	{Lat: -8.3405, Lng: 115.0920}, // Bali, Indonesia
	{Lat: 14.5995, Lng: 120.9842}, // Manila, Philippines
	{Lat: 10.3157, Lng: 123.8854}, // Cebu, Philippines
	{Lat: 6.9271, Lng: 79.8612},   // Colombo, Sri Lanka
	{Lat: 28.6139, Lng: 77.2090},  // New Delhi, India
	{Lat: 19.0760, Lng: 72.8777},  // Mumbai, India
	{Lat: 12.9716, Lng: 77.5946},  // Bengaluru, India

	{Lat: 25.2048, Lng: 55.2708}, // Dubai, UAE
	{Lat: 24.4539, Lng: 54.3773}, // Abu Dhabi, UAE
	{Lat: 25.2854, Lng: 51.5310}, // Doha, Qatar
	{Lat: 32.0853, Lng: 34.7818}, // Tel Aviv, Israel
	{Lat: 31.7683, Lng: 35.2137}, // Jerusalem, Israel
	{Lat: 31.9454, Lng: 35.9284}, // Amman, Jordan
	{Lat: 41.0082, Lng: 28.9784}, // Istanbul, Turkey
	{Lat: 39.9334, Lng: 32.8597}, // Ankara, Turkey
	{Lat: 38.4237, Lng: 27.1428}, // Izmir, Turkey
	{Lat: 36.8969, Lng: 30.7133}, // Antalya, Turkey

	{Lat: -23.5505, Lng: -46.6333}, // São Paulo, Brazil
	{Lat: -22.9068, Lng: -43.1729}, // Rio de Janeiro, Brazil
	{Lat: -15.7975, Lng: -47.8919}, // Brasília, Brazil
	{Lat: -12.9777, Lng: -38.5016}, // Salvador, Brazil
	{Lat: -25.4284, Lng: -49.2733}, // Curitiba, Brazil
	{Lat: -3.7319, Lng: -38.5267},  // Fortaleza, Brazil
	{Lat: -30.0346, Lng: -51.2177}, // Porto Alegre, Brazil
	{Lat: -34.6037, Lng: -58.3816}, // Buenos Aires, Argentina
	{Lat: -31.4201, Lng: -64.1888}, // Córdoba, Argentina
	{Lat: -32.8895, Lng: -68.8458}, // Mendoza, Argentina
	{Lat: -41.1335, Lng: -71.3103}, // Bariloche, Argentina
	{Lat: -24.7859, Lng: -65.4117}, // Salta, Argentina
	{Lat: -33.4489, Lng: -70.6693}, // Santiago, Chile
	{Lat: -33.0472, Lng: -71.6127}, // Valparaíso, Chile
	{Lat: -23.6509, Lng: -70.3975}, // Antofagasta, Chile
	{Lat: -53.1638, Lng: -70.9171}, // Punta Arenas, Chile
	{Lat: 4.7110, Lng: -74.0721},   // Bogotá, Colombia
	{Lat: 6.2442, Lng: -75.5812},   // Medellín, Colombia
	{Lat: 3.4516, Lng: -76.5320},   // Cali, Colombia
	{Lat: 10.3910, Lng: -75.4794},  // Cartagena, Colombia
	{Lat: -12.0464, Lng: -77.0428}, // Lima, Peru
	{Lat: -13.5319, Lng: -71.9675}, // Cusco, Peru
	{Lat: -16.4090, Lng: -71.5375}, // Arequipa, Peru
	{Lat: -34.9011, Lng: -56.1645}, // Montevideo, Uruguay
	{Lat: -0.1807, Lng: -78.4678},  // Quito, Ecuador
	{Lat: -2.1894, Lng: -79.8891},  // Guayaquil, Ecuador
	{Lat: -16.5000, Lng: -68.1500}, // La Paz, Bolivia
	{Lat: -17.7863, Lng: -63.1812}, // Santa Cruz, Bolivia

	{Lat: -33.8688, Lng: 151.2093}, // Sydney, Australia
	{Lat: -37.8136, Lng: 144.9631}, // Melbourne, Australia
	{Lat: -27.4698, Lng: 153.0251}, // Brisbane, Australia
	{Lat: -31.9505, Lng: 115.8605}, // Perth, Australia
	{Lat: -34.9285, Lng: 138.6007}, // Adelaide, Australia
	{Lat: -35.2809, Lng: 149.1300}, // Canberra, Australia
	{Lat: -42.8821, Lng: 147.3272}, // Hobart, Tasmania
	{Lat: -12.4634, Lng: 130.8456}, // Darwin, Australia
	{Lat: -16.9186, Lng: 145.7781}, // Cairns, Australia
	{Lat: -36.8485, Lng: 174.7633}, // Auckland, New Zealand
	{Lat: -41.2865, Lng: 174.7762}, // Wellington, New Zealand
	{Lat: -43.5321, Lng: 172.6362}, // Christchurch, New Zealand
	{Lat: -45.0312, Lng: 168.6626}, // Queenstown, New Zealand

	{Lat: -33.9249, Lng: 18.4241}, // Cape Town, South Africa
	{Lat: -26.2041, Lng: 28.0473}, // Johannesburg, South Africa
	{Lat: -29.8587, Lng: 31.0218}, // Durban, South Africa
	{Lat: -25.7479, Lng: 28.2293}, // Pretoria, South Africa
	{Lat: -1.2921, Lng: 36.8219},  // Nairobi, Kenya
	{Lat: -4.0435, Lng: 39.6682},  // Mombasa, Kenya
	{Lat: 6.5244, Lng: 3.3792},    // Lagos, Nigeria
	{Lat: 9.0765, Lng: 7.3986},    // Abuja, Nigeria
	{Lat: 5.6037, Lng: -0.1870},   // Accra, Ghana
	{Lat: 14.7167, Lng: -17.4677}, // Dakar, Senegal
	{Lat: -24.6282, Lng: 25.9231}, // Gaborone, Botswana
	{Lat: 36.8065, Lng: 10.1815},  // Tunis, Tunisia
	{Lat: -26.3054, Lng: 31.1367}, // Mbabane, Eswatini
	{Lat: -29.3151, Lng: 27.4869}, // Maseru, Lesotho
	{Lat: 0.3476, Lng: 32.5825},   // Kampala, Uganda
	{Lat: -1.9441, Lng: 30.0619},  // Kigali, Rwanda
}
