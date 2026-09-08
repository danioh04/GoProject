CREATE TABLE IF NOT EXISTS games (
	id TEXT PRIMARY KEY,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	total_rounds INT NOT NULL CHECK (total_rounds > 0)
);

CREATE TABLE IF NOT EXISTS game_players (
	game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
	player_id TEXT NOT NULL,
	nickname TEXT NOT NULL,
	total_score INT NOT NULL DEFAULT 0,
	placement INT NOT NULL CHECK (placement > 0),
	PRIMARY KEY (game_id, player_id)
);

CREATE TABLE IF NOT EXISTS rounds (
	game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
	round INT NOT NULL CHECK (round > 0),
	location_id TEXT NOT NULL,
	target_lat DOUBLE PRECISION NOT NULL,
	target_lng DOUBLE PRECISION NOT NULL,
	PRIMARY KEY (game_id, round)
);

CREATE TABLE IF NOT EXISTS guesses (
	game_id TEXT NOT NULL,
	round INT NOT NULL,
	player_id TEXT NOT NULL,
	lat DOUBLE PRECISION,
	lng DOUBLE PRECISION,
	distance_m DOUBLE PRECISION,
	score INT NOT NULL DEFAULT 0,
	PRIMARY KEY (game_id, round, player_id),
	FOREIGN KEY (game_id, round) REFERENCES rounds(game_id, round) ON DELETE CASCADE
);
