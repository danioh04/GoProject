package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr          string
	LogLevel      string
	LogFormat     string
	DatabaseURL   string
	MaxPlayers    int
	Rounds        int
	RoundSeconds  int
	RevealSeconds int
	DebugAddr     string
}

func Load() Config {
	return Config{
		Addr:          envOr("ADDR", ":8080"),
		LogLevel:      envOr("LOG_LEVEL", "info"),
		LogFormat:     envOr("LOG_FORMAT", "text"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		MaxPlayers:    envIntOr("MAX_ROOM_SIZE", 8),
		Rounds:        envIntOr("ROUNDS", 5),
		RoundSeconds:  envIntOr("ROUND_SECONDS", 60),
		RevealSeconds: envIntOr("REVEAL_SECONDS", 10),
		DebugAddr:     os.Getenv("DEBUG_ADDR"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
