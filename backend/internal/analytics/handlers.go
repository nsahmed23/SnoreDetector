// Package analytics implements per-user aggregations over the
// snore_events table.
package analytics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/metrics"
	"github.com/nsahmed23/SnoreDetector/backend/internal/ratelimit"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const tracerName = "snoreguard/analytics-service"

// Store is the subset of *store.Store the handlers depend on.
type Store interface {
	DailySummaries(ctx context.Context, userID uuid.UUID, tz string, start, end time.Time) ([]store.DailySummary, error)
	TotalsSince(ctx context.Context, userID uuid.UUID, since time.Time) (*store.Totals, error)
	IsJTIRevoked(ctx context.Context, jti string) (bool, error)
	Ping(ctx context.Context) error
}

type Deps struct {
	Logger         *slog.Logger
	Store          Store
	JWT            *authjwt.Issuer
	RequestTimeout time.Duration
	// Metrics is the service's instrument bundle. Nil is fine.
	Metrics *metrics.Instruments
}

// New builds the analytics router. Routes:
//
//	GET /healthz
//	GET /analytics/summary?start=&end=&tz=         (auth)
//	GET /analytics/totals?since=                   (auth)
func New(d Deps) http.Handler {
	if d.RequestTimeout == 0 {
		d.RequestTimeout = 15 * time.Second
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(d.RequestTimeout))
	r.Use(d.Metrics.HandlerDurationMiddleware)

	r.Get("/healthz", health(d))

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(d.JWT, d.Store))
		h := &handler{deps: d}
		// Per-user limit, separate bucket per route.
		r.With(ratelimit.PerUser(60, time.Minute)).Get("/analytics/summary", h.dailySummary)
		r.With(ratelimit.PerUser(60, time.Minute)).Get("/analytics/totals", h.totals)
	})

	return r
}

type handler struct{ deps Deps }

func health(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"status": "ok", "service": "analytics", "time": time.Now().UTC()}
		if d.Store != nil {
			if err := d.Store.Ping(r.Context()); err != nil {
				body["status"] = "degraded"
				body["db_error"] = err.Error()
				httpkit.JSON(w, http.StatusServiceUnavailable, body)
				return
			}
		}
		httpkit.JSON(w, http.StatusOK, body)
	}
}

type dailyRow struct {
	Day             string  `json:"day"` // YYYY-MM-DD in the requested timezone
	EventCount      int64   `json:"event_count"`
	TotalDurationMS int64   `json:"total_duration_ms"`
	AvgDB           float64 `json:"avg_db"`
	LongestMS       int32   `json:"longest_ms"`
}

type dailySummaryResponse struct {
	TZ   string     `json:"tz"`
	Days []dailyRow `json:"days"`
}

func (h *handler) dailySummary(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "analytics.summary",
		trace.WithAttributes(
			attribute.String("endpoint", "analytics.summary"),
			semconv.HTTPRoute("/analytics/summary"),
		),
	)
	defer span.End()

	uid, ok := auth.UserIDFrom(ctx)
	if !ok {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	span.SetAttributes(attribute.String("user_id_hash", metrics.HashUserID(uid)))
	q := r.URL.Query()
	tz := q.Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		httpkit.Error(w, http.StatusBadRequest, "unknown tz")
		return
	}
	start, end, err := parseRange(q.Get("start"), q.Get("end"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := h.deps.Store.DailySummaries(ctx, uid, tz, start, end)
	if err != nil {
		h.deps.Logger.Error("daily summaries", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		httpkit.Error(w, http.StatusInternalServerError, "failed to compute summaries")
		return
	}
	span.SetAttributes(
		attribute.Int("rows", len(rows)),
		semconv.HTTPResponseStatusCode(http.StatusOK),
	)
	out := make([]dailyRow, 0, len(rows))
	for _, d := range rows {
		out = append(out, dailyRow{
			Day:             d.Day.Format("2006-01-02"),
			EventCount:      d.EventCount,
			TotalDurationMS: d.TotalDurationMS,
			AvgDB:           d.AvgDB,
			LongestMS:       d.LongestMS,
		})
	}
	httpkit.JSON(w, http.StatusOK, dailySummaryResponse{TZ: tz, Days: out})
}

type totalsResponse struct {
	EventCount      int64      `json:"event_count"`
	TotalDurationMS int64      `json:"total_duration_ms"`
	AvgDB           float64    `json:"avg_db"`
	LongestMS       int32      `json:"longest_ms"`
	FirstEventAt    *time.Time `json:"first_event_at,omitempty"`
	LastEventAt     *time.Time `json:"last_event_at,omitempty"`
}

func (h *handler) totals(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "analytics.totals",
		trace.WithAttributes(
			attribute.String("endpoint", "analytics.totals"),
			semconv.HTTPRoute("/analytics/totals"),
		),
	)
	defer span.End()

	uid, ok := auth.UserIDFrom(ctx)
	if !ok {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	span.SetAttributes(attribute.String("user_id_hash", metrics.HashUserID(uid)))
	since := time.Time{}
	if s := r.URL.Query().Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			httpkit.Error(w, http.StatusBadRequest, "invalid 'since' (want RFC3339)")
			return
		}
		since = t
	}
	totals, err := h.deps.Store.TotalsSince(ctx, uid, since)
	if err != nil {
		h.deps.Logger.Error("totals", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		httpkit.Error(w, http.StatusInternalServerError, "failed to compute totals")
		return
	}
	span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusOK))
	httpkit.JSON(w, http.StatusOK, totalsResponse{
		EventCount:      totals.EventCount,
		TotalDurationMS: totals.TotalDurationMS,
		AvgDB:           totals.AvgDB,
		LongestMS:       totals.LongestMS,
		FirstEventAt:    totals.FirstEventAt,
		LastEventAt:     totals.LastEventAt,
	})
}

func parseRange(rawStart, rawEnd string) (time.Time, time.Time, error) {
	if rawStart == "" || rawEnd == "" {
		return time.Time{}, time.Time{}, errors.New("'start' and 'end' are required (RFC3339)")
	}
	start, err := time.Parse(time.RFC3339, rawStart)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid 'start' (want RFC3339)")
	}
	end, err := time.Parse(time.RFC3339, rawEnd)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid 'end' (want RFC3339)")
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errors.New("'end' must be after 'start'")
	}
	if end.Sub(start) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("range must be ≤ 366 days")
	}
	return start, end, nil
}

