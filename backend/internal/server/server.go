// Package server wires the chi router for the sync service.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/apple"
	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/blobstore"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/ratelimit"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

// Store is the subset of *store.Store the handlers depend on. Defined
// here (consumer side) so tests can plug in fakes without spinning up
// Postgres.
type Store interface {
	UpsertUser(ctx context.Context, appleSubject, email string, isPrivateEmail bool) (*store.User, error)
	InsertEvents(ctx context.Context, userID uuid.UUID, evs []store.SnoreEvent) (int, error)
	ListEvents(ctx context.Context, userID uuid.UUID, cursor store.Cursor, limit int) ([]store.SnoreEvent, error)
	Ping(ctx context.Context) error

	// Refresh-token + revocation surface.
	RecordRefreshToken(ctx context.Context, tokenID, userID, familyID uuid.UUID, expiresAt time.Time) error
	GetRefreshToken(ctx context.Context, tokenID uuid.UUID) (*store.RefreshToken, error)
	ReplaceRefreshToken(ctx context.Context, oldID, newID, userID, familyID uuid.UUID, expiresAt time.Time) error
	RevokeRefreshFamily(ctx context.Context, userID, familyID uuid.UUID) error
	RecordRevokedJTI(ctx context.Context, jti string, userID uuid.UUID) error
	IsJTIRevoked(ctx context.Context, jti string) (bool, error)

	// Recording-session + audio-clip surface (Phase B).
	UpsertSession(ctx context.Context, userID uuid.UUID, in store.RecordingSession) (*store.RecordingSession, bool, error)
	ListSessions(ctx context.Context, userID uuid.UUID, cursor store.DescCursor, limit int) ([]store.RecordingSession, error)
	UpsertAudioClip(ctx context.Context, userID uuid.UUID, in store.AudioClip) (*store.AudioClip, bool, error)
	ListAudioClips(ctx context.Context, userID uuid.UUID, cursor store.DescCursor, limit int) ([]store.AudioClip, error)
	GetAudioClip(ctx context.Context, userID, clipID uuid.UUID) (*store.AudioClip, error)
	SoftDeleteAudioClip(ctx context.Context, userID, clipID uuid.UUID) error
}

type Deps struct {
	Logger         *slog.Logger
	Store          Store
	Apple          *apple.Verifier
	JWT            *authjwt.Issuer
	RequestTimeout time.Duration
	// Now lets tests freeze time. Defaults to time.Now.
	Now func() time.Time

	// BlobStore holds the raw audio bodies. Handlers fail closed
	// (500) when this is nil but the route is hit; main.go MUST
	// supply one.
	BlobStore blobstore.Store
	// AudioMaxClipBytes caps the per-clip body size. Zero falls back
	// to 5 MiB inside the handler.
	AudioMaxClipBytes int64
	// AudioAllowedMIME is the whitelist of declared+sniffed
	// content-types accepted on upload. Empty falls back to the
	// default audio/m4a, audio/mp4, audio/wav, audio/aac set.
	AudioAllowedMIME []string
}

// New constructs the chi router with all routes wired up.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.RequestTimeout == 0 {
		d.RequestTimeout = 15 * time.Second
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(d.RequestTimeout))

	r.Get("/healthz", healthHandler(d))

	authH := &authHandler{deps: d}
	// Per-IP limits on the unauthenticated auth endpoints. Each
	// route gets its own bucket via r.With(...) — using r.Use(...)
	// here would share one limiter across all auth routes.
	r.With(ratelimit.PerIP(10, time.Minute)).Post("/auth/apple", authH.handle)
	r.With(ratelimit.PerIP(30, time.Minute)).Post("/auth/refresh", authH.refresh)

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(d.JWT, d.Store))
		// Per-user limits on authenticated routes. /auth/logout is
		// authenticated but stays cheap so we cap it at the same
		// rate as /auth/refresh for symmetry.
		r.With(ratelimit.PerUser(30, time.Minute)).Post("/auth/logout", authH.logout)
		evH := &eventsHandler{deps: d}
		r.With(ratelimit.PerUser(200, time.Minute)).Post("/events", evH.create)
		r.With(ratelimit.PerUser(200, time.Minute)).Get("/events", evH.list)

		// Phase B: recording sessions + audio clip cloud sync.
		sessH := &sessionsHandler{deps: d}
		r.With(ratelimit.PerUser(60, time.Minute)).Post("/sessions", sessH.create)
		r.With(ratelimit.PerUser(60, time.Minute)).Get("/sessions", sessH.list)

		clipH := &clipsHandler{deps: d}
		// Upload is the heaviest call (bytes ingest + sha256 + blob
		// write). Tighter cap than the read paths.
		r.With(ratelimit.PerUser(30, time.Minute)).Post("/audio/clips", clipH.create)
		r.With(ratelimit.PerUser(60, time.Minute)).Get("/audio/clips", clipH.list)
		// Download is the most expensive read (full body egress);
		// keep it on a per-hour budget so a runaway script can't
		// drain a user's bandwidth allowance.
		r.With(ratelimit.PerUser(10, time.Hour)).Get("/audio/clips/{id}/download", clipH.download)
		r.With(ratelimit.PerUser(60, time.Minute)).Delete("/audio/clips/{id}", clipH.delete)
	})

	return r
}

func healthHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := map[string]any{"status": "ok", "time": d.Now().UTC()}
		if d.Store != nil {
			if err := d.Store.Ping(r.Context()); err != nil {
				status["status"] = "degraded"
				status["db_error"] = err.Error()
				httpkit.JSON(w, http.StatusServiceUnavailable, status)
				return
			}
		}
		httpkit.JSON(w, http.StatusOK, status)
	}
}
