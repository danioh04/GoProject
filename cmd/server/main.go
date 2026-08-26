package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"geoduel/internal/api"
	"geoduel/internal/config"
	"geoduel/internal/game"
	"geoduel/internal/hub"
	"geoduel/internal/location"
	"geoduel/internal/room"
	"geoduel/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg := config.Load()
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	pool, err := location.Load()
	if err != nil {
		return fmt.Errorf("load locations: %w", err)
	}
	logger.Info("location pool loaded", "count", pool.Len())

	gameCfg := game.DefaultConfig()
	gameCfg.Rounds = cfg.Rounds
	gameCfg.RoundTime = time.Duration(cfg.RoundSeconds) * time.Second
	gameCfg.RevealTime = time.Duration(cfg.RevealSeconds) * time.Second

	var pgStore *store.Store
	var persistence api.Persistence
	if cfg.DatabaseURL != "" {
		bootCtx, cancelBoot := context.WithTimeout(context.Background(), 10*time.Second)
		pgStore, err = store.Open(bootCtx, cfg.DatabaseURL)
		cancelBoot()
		if err != nil {
			return fmt.Errorf("connect to database: %w", err)
		}
		defer pgStore.Close()
		if err := pgStore.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
		persistence = pgStore
		logger.Info("postgres ready")
	} else {
		logger.Info("postgres disabled", "reason", "DATABASE_URL not set")
	}

	opts := room.Options{
		MaxPlayers: cfg.MaxPlayers,
		Picker:     pool.Picker(nil),
		Config:     gameCfg,
	}
	if pgStore != nil {
		opts.Store = pgStore
	}

	rooms := hub.New(logger, opts)

	srv := &http.Server{
		Handler:           api.New(logger, rooms, persistence),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", listener.Addr().String())
		serverErrors <- srv.Serve(listener)
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("stopping rooms")
	if closed := rooms.Shutdown(3 * time.Second); closed > 0 {
		logger.Info("rooms stopped", "count", closed)
	}
	logger.Info("stopped")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	level := new(slog.LevelVar)
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level.Set(slog.LevelInfo)
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}
