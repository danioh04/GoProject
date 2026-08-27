package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
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
	DebugAddr        string
	GoogleMapsAPIKey string
	MapFile          string
}

func Load() Config {
	loadDotEnv(".env")
	return Config{
		Addr:             envOr("ADDR", ":8080"),
		LogLevel:         envOr("LOG_LEVEL", "info"),
		LogFormat:        envOr("LOG_FORMAT", "text"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		MaxPlayers:       envIntOr("MAX_ROOM_SIZE", 8),
		Rounds:           envIntOr("ROUNDS", 5),
		RoundSeconds:     envIntOr("ROUND_SECONDS", 60),
		RevealSeconds:    envIntOr("REVEAL_SECONDS", 10),
		DebugAddr:        os.Getenv("DEBUG_ADDR"),
		GoogleMapsAPIKey: os.Getenv("GOOGLE_MAPS_API_KEY"),
		MapFile:          os.Getenv("MAP_FILE"),
	}
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		} else {
			if idx := strings.Index(val, " #"); idx != -1 {
				val = strings.TrimSpace(val[:idx])
			} else if idx := strings.Index(val, "\t#"); idx != -1 {
				val = strings.TrimSpace(val[:idx])
			}
		}

		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	if err := scanner.Err(); err != nil {
		return
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
