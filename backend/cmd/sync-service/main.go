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
//   AUDIO_MAX_CLIP_BYTES per-clip body cap, default 5*1024*1024 (5 MiB)
//   AUDIO_ALLOWED_MIME comma-separated MIME allowlist for clip uploads
//   BLOBSTORE_BACKEND  "filesystem" (default) | "fake" (tests only) | "gcs" (v1 prod)
//   BLOBSTORE_FS_ROOT  filesystem-backend root, default "./var/blobstore"
//   BLOBSTORE_GCS_BUCKET GCS bucket name (required when BLOBSTORE_BACKEND=gcs)
//   BLOBSTORE_GCS_PREFIX GCS object key prefix, default "clips/"
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/nsahmed23/SnoreDetector/backend/internal/apple"
	"github.com/nsahmed23/SnoreDetector/backend/internal/blobstore"
	"github.com/nsahmed23/SnoreDetector/backend/internal/config"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	otelsetup "github.com/nsahmed23/SnoreDetector/backend/internal/otel"
	"github.com/nsahmed23/SnoreDetector/backend/internal/server"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
	"github.com/nsahmed23/SnoreDetector/backend/migrations"
)

const serviceName = "sync-service"

var Version = "dev"

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

	blob, err := buildBlobstore(rootCtx, cfg)
	if err != nil {
		return err
	}

	handler := server.New(server.Deps{
		Logger:            logger,
		Store:             st,
		Apple:             verifier,
		JWT:               jwtIss,
		RequestTimeout:    cfg.RequestTimeout,
		BlobStore:         blob,
		AudioMaxClipBytes: cfg.AudioMaxClipBytes,
		AudioAllowedMIME:  cfg.AudioAllowedMIME,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           otelhttp.NewHandler(handler, "sync"),
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

// buildBlobstore picks the configured backend. We deliberately fail
// closed on unknown / unimplemented backends so a typo in
// BLOBSTORE_BACKEND can't quietly start the service with no cloud
// storage attached. GCS is the v1 production target (ADR 0008); S3
// remains valid future work behind the same Store interface.
func buildBlobstore(ctx context.Context, cfg *config.SyncService) (blobstore.Store, error) {
	switch cfg.BlobstoreBackend {
	case "filesystem", "":
		if cfg.BlobstoreFSRoot == "" {
			return nil, errors.New("BLOBSTORE_FS_ROOT is required when BLOBSTORE_BACKEND=filesystem")
		}
		return blobstore.NewFilesystem(cfg.BlobstoreFSRoot), nil
	case "fake":
		// Allowed for local smoke / fuzz; in production env-var
		// validation upstream should prevent this from leaking.
		return blobstore.NewFake(), nil
	case "gcs":
		// Prefix is normalized so callers don't have to remember the
		// trailing-slash convention (BLOBSTORE_GCS_PREFIX="clips" and
		// "clips/" both produce the same key shape).
		gcs, err := blobstore.NewGCS(ctx, cfg.BlobstoreGCSBucket, blobstore.NormalizePrefix(cfg.BlobstoreGCSPrefix))
		if err != nil {
			return nil, fmt.Errorf("blobstore: %w", err)
		}
		return gcs, nil
	case "s3":
		return nil, fmt.Errorf("blobstore backend %q not implemented in this PR (see ADR 0008)", cfg.BlobstoreBackend)
	default:
		return nil, fmt.Errorf("unknown BLOBSTORE_BACKEND=%q (want filesystem|fake|gcs)", cfg.BlobstoreBackend)
	}
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
