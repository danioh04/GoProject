package config

import "os"

type Config struct {
	Addr        string
	LogLevel    string
	LogFormat   string
	DatabaseURL string
}

func Load() Config {
	return Config{
		Addr:        envOr("ADDR", ":8080"),
		LogLevel:    envOr("LOG_LEVEL", "info"),
		LogFormat:   envOr("LOG_FORMAT", "text"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
