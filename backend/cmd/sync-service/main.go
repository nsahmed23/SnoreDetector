// sync-service: receives Apple Sign-In, issues session JWTs, and
// brokers cross-device snore-event sync over Postgres.
//
// Configuration is environment-driven (12-factor):
//
//   ADDR              listen address, default ":8080"
//   DATABASE_URL      Postgres connection URL (required)
//   APPLE_AUDIENCE    iOS bundle ID, default "com.snoreguard.app"
//   APPLE_ISSUER      JWT iss, default "https://appleid.apple.com"
//   JWT_SIGNING_KEY   ≥32-byte HS256 key for session tokens (required)
//   JWT_ISSUER        access token iss, default "snoreguard-sync"
//   JWT_REFRESH_ISSUER refresh token iss, default "snoreguard-refresh"
//   JWT_TTL           access token lifetime, default 1h
//   JWT_REFRESH_TTL   refresh token lifetime, default 1440h (60d)
//   LOG_LEVEL         debug|info|warn|error, default info
//   REQUEST_TIMEOUT   per-request timeout, default 15s
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nsahmed23/SnoreDetector/backend/internal/apple"
	"github.com/nsahmed23/SnoreDetector/backend/internal/config"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/server"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
	"github.com/nsahmed23/SnoreDetector/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("sync-service exited with error", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadSyncService()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Run migrations on a single non-pooled connection.
	migConn, err := store.AcquireConn(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if err := migrations.Up(rootCtx, migConn); err != nil {
		_ = migConn.Close(rootCtx)
		return err
	}
	if err := migConn.Close(rootCtx); err != nil {
		logger.Warn("close migration conn", "err", err.Error())
	}

	pool, err := store.Connect(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)

	verifier, err := apple.New(rootCtx, cfg.AppleAudience, cfg.AppleIssuer)
	if err != nil {
		return err
	}
	jwtIss, err := authjwt.NewWithRefresh(
		cfg.JWTSigningKey,
		cfg.JWTIssuer,
		cfg.JWTRefreshIssuer,
		cfg.JWTTTL,
		cfg.JWTRefreshTTL,
	)
	if err != nil {
		return err
	}

	handler := server.New(server.Deps{
		Logger:         logger,
		Store:          st,
		Apple:          verifier,
		JWT:            jwtIss,
		RequestTimeout: cfg.RequestTimeout,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.RequestTimeout + 5*time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		return err
	}

	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	return srv.Shutdown(shutdownCtx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
