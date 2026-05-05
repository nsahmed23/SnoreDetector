// analytics-service: per-user aggregations over the snore_events table.
//
// Read-only — all writes happen through sync-service.
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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/nsahmed23/SnoreDetector/backend/internal/analytics"
	"github.com/nsahmed23/SnoreDetector/backend/internal/config"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	otelsetup "github.com/nsahmed23/SnoreDetector/backend/internal/otel"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const serviceName = "analytics-service"

func main() {
	if err := run(); err != nil {
		slog.Error(serviceName+" exited with error", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAnalyticsService()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	otelShutdown, err := otelsetup.Setup(rootCtx, serviceName, version())
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := otelShutdown(shutdownCtx); err != nil {
			logger.Warn("otel shutdown", "err", err.Error())
		}
	}()

	pool, err := store.Connect(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)

	jwtIss, err := authjwt.New(cfg.JWTSigningKey, cfg.JWTIssuer, cfg.JWTTTL)
	if err != nil {
		return err
	}

	handler := analytics.New(analytics.Deps{
		Logger:         logger,
		Store:          st,
		JWT:            jwtIss,
		RequestTimeout: cfg.RequestTimeout,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           otelhttp.NewHandler(handler, "analytics"),
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

// version is built-in to the binary at build time via -ldflags.
// Falls back to "dev" otherwise.
var Version = "dev"

func version() string { return Version }

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
