// export-service: streams a user's snore-event history as CSV/JSON
// for backup or GDPR portability.
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

	"github.com/nsahmed23/SnoreDetector/backend/internal/config"
	"github.com/nsahmed23/SnoreDetector/backend/internal/export"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/metrics"
	otelsetup "github.com/nsahmed23/SnoreDetector/backend/internal/otel"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const serviceName = "export-service"

var Version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error(serviceName+" exited with error", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadExportService()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	otelShutdown, err := otelsetup.Setup(rootCtx, serviceName, Version)
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

	instr, err := metrics.New(serviceName)
	if err != nil {
		return err
	}

	pool, err := store.Connect(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool).WithMetrics(instr)

	jwtIss, err := authjwt.New(cfg.JWTSigningKey, cfg.JWTIssuer, cfg.JWTTTL)
	if err != nil {
		return err
	}
	jwtIss.WithMetrics(instr)

	handler := export.New(export.Deps{
		Logger:         logger,
		Store:          st,
		JWT:            jwtIss,
		RequestTimeout: cfg.RequestTimeout,
		Metrics:        instr,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           otelhttp.NewHandler(handler, "export"),
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
