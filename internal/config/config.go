package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr             string
	LogLevel         string
	LogFormat        string
	DatabaseURL      string
	MaxPlayers       int
	Rounds           int
	RoundSeconds     int
	RevealSeconds    int
	GoogleMapsAPIKey string
}

// Load reads configuration settings from environment variables with fallback defaults.
func Load() Config {
	return Config{
		Addr:             envOr("ADDR", ":8080"),
		LogLevel:         envOr("LOG_LEVEL", "info"),
		LogFormat:        envOr("LOG_FORMAT", "text"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		MaxPlayers:       envIntOr("MAX_PLAYERS", 8),
		Rounds:           envIntOr("ROUNDS", 5),
		RoundSeconds:     envIntOr("ROUND_SECONDS", 60),
		RevealSeconds:    envIntOr("REVEAL_SECONDS", 10),
		GoogleMapsAPIKey: os.Getenv("GOOGLE_MAPS_API_KEY"),
	}
}

// envOr retrieves an environment variable or returns the provided fallback if unset.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

// envIntOr parses an integer environment variable or returns the provided fallback if unset or invalid.
func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}

	return fallback
}
