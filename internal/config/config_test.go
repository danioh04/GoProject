package config_test

import (
	"testing"

	"geoduel/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	// Clear relevant env vars
	for _, k := range []string{"ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL", "MAX_ROOM_SIZE", "ROUNDS", "ROUND_SECONDS", "REVEAL_SECONDS", "DEBUG_ADDR", "GOOGLE_MAPS_API_KEY"} {
		t.Setenv(k, "")
	}

	cfg := config.Load()
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.LogLevel != "info" || cfg.LogFormat != "text" {
		t.Errorf("Log settings = %q/%q, want info/text", cfg.LogLevel, cfg.LogFormat)
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.MaxPlayers != 8 || cfg.Rounds != 5 || cfg.RoundSeconds != 60 || cfg.RevealSeconds != 10 {
		t.Errorf("game timing wrong: %+v", cfg)
	}
	if cfg.DebugAddr != "" {
		t.Errorf("DebugAddr = %q, want empty", cfg.DebugAddr)
	}
	if cfg.GoogleMapsAPIKey != "" {
		t.Errorf("GoogleMapsAPIKey = %q, want empty", cfg.GoogleMapsAPIKey)
	}
}

func TestLoadCustomValues(t *testing.T) {
	t.Setenv("ADDR", ":9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("MAX_ROOM_SIZE", "12")
	t.Setenv("ROUNDS", "3")
	t.Setenv("ROUND_SECONDS", "30")
	t.Setenv("REVEAL_SECONDS", "5")
	t.Setenv("DEBUG_ADDR", ":6060")
	t.Setenv("GOOGLE_MAPS_API_KEY", "AIzaSyTestKey123")

	cfg := config.Load()
	if cfg.Addr != ":9090" || cfg.LogLevel != "debug" || cfg.LogFormat != "json" {
		t.Errorf("custom server settings wrong: %+v", cfg)
	}
	if cfg.DatabaseURL != "postgres://localhost/test" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.MaxPlayers != 12 || cfg.Rounds != 3 || cfg.RoundSeconds != 30 || cfg.RevealSeconds != 5 {
		t.Errorf("custom game settings wrong: %+v", cfg)
	}
	if cfg.DebugAddr != ":6060" {
		t.Errorf("DebugAddr = %q, want :6060", cfg.DebugAddr)
	}
	if cfg.GoogleMapsAPIKey != "AIzaSyTestKey123" {
		t.Errorf("GoogleMapsAPIKey = %q, want AIzaSyTestKey123", cfg.GoogleMapsAPIKey)
	}
}

func TestLoadInvalidIntFallback(t *testing.T) {
	t.Setenv("MAX_ROOM_SIZE", "not-a-number")
	t.Setenv("ROUNDS", "-5")
	t.Setenv("ROUND_SECONDS", "0")

	cfg := config.Load()
	if cfg.MaxPlayers != 8 {
		t.Errorf("MaxPlayers = %d, want 8 (fallback for NaN)", cfg.MaxPlayers)
	}
	if cfg.Rounds != 5 {
		t.Errorf("Rounds = %d, want 5 (fallback for negative)", cfg.Rounds)
	}
	if cfg.RoundSeconds != 60 {
		t.Errorf("RoundSeconds = %d, want 60 (fallback for zero)", cfg.RoundSeconds)
	}
}
