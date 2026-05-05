// Package server wires the chi router for the sync service.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/apple"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

// Store is the subset of *store.Store the handlers depend on. Defined
// here (consumer side) so tests can plug in fakes without spinning up
// Postgres.
type Store interface {
	UpsertUser(ctx context.Context, appleSubject, email string, isPrivateEmail bool) (*store.User, error)
	InsertEvents(ctx context.Context, userID uuid.UUID, evs []store.SnoreEvent) (int, error)
	ListEvents(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]store.SnoreEvent, error)
	Ping(ctx context.Context) error
}

type Deps struct {
	Logger         *slog.Logger
	Store          Store
	Apple          *apple.Verifier
	JWT            *authjwt.Issuer
	RequestTimeout time.Duration
	// Now lets tests freeze time. Defaults to time.Now.
	Now func() time.Time
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
	r.Use(jsonContentType)

	r.Get("/healthz", healthHandler(d))

	authH := &authHandler{deps: d}
	r.Post("/auth/apple", authH.handle)

	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(d.JWT))
		evH := &eventsHandler{deps: d}
		r.Post("/events", evH.create)
		r.Get("/events", evH.list)
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
				writeJSON(w, http.StatusServiceUnavailable, status)
				return
			}
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
