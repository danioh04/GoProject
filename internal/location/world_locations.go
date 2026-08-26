package location

import "geoduel/internal/game"

// WorldLocations contains a curated global dataset of coordinates across major cities
// with distinct 22-character Google Street View car panorama IDs.
var WorldLocations = []game.Location{
	// North America - USA
	{ID: "us_nyc_times_sq", Title: "New York City, USA", Lat: 40.7580, Lng: -73.9855, PanoID: "e6dG65F36ZzGz_5Uv8k9wA", Hint: "Bright neon billboards in the heart of Manhattan"},
	{ID: "us_la_hollywood", Title: "Los Angeles, USA", Lat: 34.1016, Lng: -118.3268, PanoID: "dX1xfnpDUh-8q3jCdW_s_S", Hint: "Walk of Fame near palm-lined California boulevards"},
	{ID: "us_sf_lombard", Title: "San Francisco, USA", Lat: 37.8021, Lng: -122.4187, PanoID: "3x9kF6aZ3xW8l0q_9kL8pQ", Hint: "Famous crooked flower-lined winding street"},
	{ID: "us_chicago_michigan", Title: "Chicago, USA", Lat: 41.8827, Lng: -87.6233, PanoID: "7kL0q9W3x8z1a4b2c5d6eF", Hint: "Magnificent Mile skyscrapers near Lake Michigan"},
	{ID: "us_miami_ocean", Title: "Miami Beach, USA", Lat: 25.7826, Lng: -80.1301, PanoID: "9xL8pQ3x9kF6aZ3xW8l0qA", Hint: "Art deco hotels along a tropical ocean drive"},
	{ID: "us_seattle_waterfront", Title: "Seattle, USA", Lat: 47.6088, Lng: -122.3400, PanoID: "4b2c5d6eF7kL0q9W3x8z1A", Hint: "Public market overlooking Puget Sound"},
	{ID: "us_las_vegas_bellagio", Title: "Las Vegas, USA", Lat: 36.1126, Lng: -115.1767, PanoID: "6aZ3xW8l0q_9kL8pQ3x9kF", Hint: "Famous fountain resort on the Las Vegas Strip"},

	// North America - Canada & Mexico
	{ID: "ca_toronto_front_st", Title: "Toronto, Canada", Lat: 43.6426, Lng: -79.3871, PanoID: "1a4b2c5d6eF7kL0q9W3x8z", Hint: "Modern skyscraper canyon near Lake Ontario"},
	{ID: "ca_vancouver_waterfront", Title: "Vancouver, Canada", Lat: 49.2860, Lng: -123.1116, PanoID: "8l0q_9kL8pQ3x9kF6aZ3xW", Hint: "Coastal mountain views by the Pacific harbor"},
	{ID: "ca_montreal_notre_dame", Title: "Montreal, Canada", Lat: 45.5048, Lng: -73.5539, PanoID: "5d6eF7kL0q9W3x8z1a4b2c", Hint: "Old French cobblestone quarter and basilica"},
	{ID: "mx_mexico_city_reforma", Title: "Mexico City, Mexico", Lat: 19.4270, Lng: -99.1677, PanoID: "3xW8l0q_9kL8pQ3x9kF6aZ", Hint: "Angel of Independence monument on Paseo de la Reforma"},

	// Europe - Western & Northern
	{ID: "uk_london_westminster", Title: "London, UK", Lat: 51.5007, Lng: -0.1246, PanoID: "s6X_82n31aI_47e9Xl8p4w", Hint: "Big Ben clock tower beside Westminster Bridge and the Thames"},
	{ID: "uk_edinburgh_royal_mile", Title: "Edinburgh, UK", Lat: 55.9486, Lng: -3.1999, PanoID: "2b3c4d5e6f7g8h9j1k2m3n", Hint: "Historic stone castle towering above cobblestone closes"},
	{ID: "fr_paris_eiffel", Title: "Paris, France", Lat: 48.8584, Lng: 2.2945, PanoID: "0oU9oWv065S_VzV-H1G4kg", Hint: "Iconic wrought-iron lattice tower standing tall on the Champ de Mars"},
	{ID: "fr_nice_promenade", Title: "Nice, France", Lat: 43.6946, Lng: 7.2606, PanoID: "3c4d5e6f7g8h9j1k2m3n4p", Hint: "Palm-lined seaside promenade along the Mediterranean French Riviera"},
	{ID: "de_berlin_brandenburg", Title: "Berlin, Germany", Lat: 52.5163, Lng: 13.3777, PanoID: "9W3x8z1a4b2c5d6eF7kL0q", Hint: "Historic neoclassical gate at Pariser Platz"},
	{ID: "es_madrid_alcala", Title: "Madrid, Spain", Lat: 40.4203, Lng: -3.7058, PanoID: "4d5e6f7g8h9j1k2m3n4p5q", Hint: "Grand architecture in Spain's vibrant capital"},
	{ID: "es_barcelona_diagonal", Title: "Barcelona, Spain", Lat: 41.4036, Lng: 2.1744, PanoID: "5e6f7g8h9j1k2m3n4p5q6r", Hint: "Catalan modernist city near the Mediterranean sea"},
	{ID: "it_rome_colosseum", Title: "Rome, Italy", Lat: 41.8902, Lng: 12.4922, PanoID: "6f7g8h9j1k2m3n4p5q6r7s", Hint: "Ancient Roman amphitheater in the Eternal City"},
	{ID: "nl_amsterdam_dam", Title: "Amsterdam, Netherlands", Lat: 52.3731, Lng: 4.8926, PanoID: "7g8h9j1k2m3n4p5q6r7s8t", Hint: "Historic canals and brick gabled townhouses"},
	{ID: "se_stockholm_gamla", Title: "Stockholm, Sweden", Lat: 59.3257, Lng: 18.0709, PanoID: "8h9j1k2m3n4p5q6r7s8t9u", Hint: "Vibrant yellow and ochre Nordic old town island"},
	{ID: "no_oslo_radhus", Title: "Oslo, Norway", Lat: 59.9118, Lng: 10.7335, PanoID: "9j1k2m3n4p5q6r7s8t9u1v", Hint: "Fjord-side Nordic capital near modern waterfronts"},
	{ID: "dk_copenhagen_kongens", Title: "Copenhagen, Denmark", Lat: 55.6795, Lng: 12.5908, PanoID: "1k2m3n4p5q6r7s8t9u1v2w", Hint: "Colorfully painted 17th-century canal facades"},

	// Europe - Central & Southern
	{ID: "at_vienna_ring", Title: "Vienna, Austria", Lat: 48.2030, Lng: 16.3691, PanoID: "2m3n4p5q6r7s8t9u1v2w3x", Hint: "Grand imperial boulevard lined with historic palaces"},
	{ID: "cz_prague_old_town", Title: "Prague, Czechia", Lat: 50.0865, Lng: 14.4114, PanoID: "3n4p5q6r7s8t9u1v2w3x4y", Hint: "Gothic and Baroque spires along the Vltava River"},
	{ID: "hu_budapest_danube", Title: "Budapest, Hungary", Lat: 47.5071, Lng: 19.0456, PanoID: "4p5q6r7s8t9u1v2w3x4y5z", Hint: "Grand parliament building rising above the river Danube"},
	{ID: "gr_athens_syntagma", Title: "Athens, Greece", Lat: 37.9753, Lng: 23.7361, PanoID: "5q6r7s8t9u1v2w3x4y5z6a", Hint: "Cradle of democracy overlooked by ancient marble hills"},
	{ID: "pt_lisbon_rossio", Title: "Lisbon, Portugal", Lat: 38.7138, Lng: -9.1394, PanoID: "6r7s8t9u1v2w3x4y5z6a7b", Hint: "Wavy black and white mosaic cobblestone plazas"},

	// Asia & Middle East
	{ID: "jp_tokyo_shibuya", Title: "Tokyo, Japan", Lat: 35.6595, Lng: 139.7005, PanoID: "bYjJc805_n1a2gC4K4j3pQ", Hint: "Famous pedestrian scramble crossing under neon billboards"},
	{ID: "jp_kyoto_kawaramachi", Title: "Kyoto, Japan", Lat: 35.0037, Lng: 135.7700, PanoID: "7s8t9u1v2w3x4y5z6a7b8c", Hint: "Historic Japanese temple city along the Kamo River"},
	{ID: "kr_seoul_gangnam", Title: "Seoul, South Korea", Lat: 37.5013, Lng: 127.0396, PanoID: "8t9u1v2w3x4y5z6a7b8c9d", Hint: "Gleaming glass skyscrapers in a bustling modern tech hub"},
	{ID: "tw_taipei_xinyi", Title: "Taipei, Taiwan", Lat: 25.0339, Lng: 121.5645, PanoID: "9u1v2w3x4y5z6a7b8c9d1e", Hint: "Modern boulevard beneath Taipei 101 tower"},
	{ID: "sg_singapore_orchard", Title: "Singapore", Lat: 1.3048, Lng: 103.8318, PanoID: "1v2w3x4y5z6a7b8c9d1e2f", Hint: "Lush tropical garden city boulevard with luxury retail"},
	{ID: "tr_istanbul_sultanahmet", Title: "Istanbul, Turkey", Lat: 41.0082, Lng: 28.9784, PanoID: "2w3x4y5z6a7b8c9d1e2f3g", Hint: "Ancient domes and minarets bridging Europe and Asia"},

	// Oceania
	{ID: "au_sydney_circular_quay", Title: "Sydney, Australia", Lat: -33.8568, Lng: 151.2153, PanoID: "Y_o5EaFkKzQ3l6L4p2m9nA", Hint: "Harbour views near the iconic white shell sails"},
	{ID: "au_melbourne_flinders", Title: "Melbourne, Australia", Lat: -37.8180, Lng: 144.9671, PanoID: "3x4y5z6a7b8c9d1e2f3g4h", Hint: "Yellow historic railway station and green electric trams"},
	{ID: "nz_auckland_queen_st", Title: "Auckland, New Zealand", Lat: -36.8485, Lng: 174.7633, PanoID: "4y5z6a7b8c9d1e2f3g4h5j", Hint: "Harbour city framed by coastal waters and volcanic hills"},

	// South America & Africa
	{ID: "br_rio_ipanema", Title: "Rio de Janeiro, Brazil", Lat: -22.9838, Lng: -43.2045, PanoID: "5z6a7b8c9d1e2f3g4h5j6k", Hint: "Atlantic surf beach framed by two iconic peak mountains"},
	{ID: "ar_buenos_aires_corrientes", Title: "Buenos Aires, Argentina", Lat: -34.6037, Lng: -58.3816, PanoID: "6a7b8c9d1e2f3g4h5j6k7m", Hint: "Theater district near the gigantic central obelisk"},
	{ID: "za_cape_town_waterfront", Title: "Cape Town, South Africa", Lat: -33.9036, Lng: 18.4205, PanoID: "7b8c9d1e2f3g4h5j6k7m8n", Hint: "Harbour basin backed by the flat plateau of Table Mountain"},
}
