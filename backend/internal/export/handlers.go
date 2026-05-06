// Package export streams the user's snore-event history as CSV or
// JSON for backup / GDPR portability.
//
// Streams rows directly to the response writer in pages of
// `pageSize` so even multi-month exports stay flat in memory.
package export

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/ratelimit"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const pageSize = 500

// Store is the subset of *store.Store this handler depends on.
type Store interface {
	ListEvents(ctx context.Context, userID uuid.UUID, cursor store.Cursor, limit int) ([]store.SnoreEvent, error)
	IsJTIRevoked(ctx context.Context, jti string) (bool, error)
	Ping(ctx context.Context) error

	// Per-user export budget surface.
	RecordExport(ctx context.Context, userID uuid.UUID, format string, bytesSent *int64, statusCode int) error
	CountExportsInWindow(ctx context.Context, userID uuid.UUID, since time.Time) (int, error)
	OldestExportInWindow(ctx context.Context, userID uuid.UUID, since time.Time) (time.Time, error)
}

type Deps struct {
	Logger         *slog.Logger
	Store          Store
	JWT            *authjwt.Issuer
	RequestTimeout time.Duration

	// Budget is the maximum number of exports a single user may
	// run inside Window. Zero disables the budget entirely (unit
	// tests that don't care about it can leave it zero).
	Budget int
	// Window is the rolling-window length the budget is enforced
	// over. Zero defaults to 24h.
	Window time.Duration
	// Now lets tests freeze time. Defaults to time.Now.
	Now func() time.Time
}

func New(d Deps) http.Handler {
	if d.RequestTimeout == 0 {
		// Exports can take a while; widen the default.
		d.RequestTimeout = 60 * time.Second
	}
	if d.Window == 0 {
		d.Window = 24 * time.Hour
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(d.RequestTimeout))

	r.Get("/healthz", health(d))

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(d.JWT, d.Store))
		h := &handler{deps: d}
		// Per-user limit AND per-user budget — the rate limiter is
		// short-window (10/hour) bot defense, the budget is the
		// hard 24h cap on data egress.
		r.With(ratelimit.PerUser(10, time.Hour)).Get("/export/events.csv", h.csv)
		r.With(ratelimit.PerUser(10, time.Hour)).Get("/export/events.json", h.json)
	})
	return r
}

type handler struct{ deps Deps }

func health(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"status": "ok", "service": "export", "time": time.Now().UTC()}
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

func (h *handler) csv(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.checkBudgetAndMaybeReject(w, r, uid) {
		return
	}
	ww := wrapWriter(w)
	w = ww
	defer h.recordAudit(r, uid, "csv", ww)

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	max, err := parseMax(r.URL.Query().Get("max"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="snoreguard-events.csv"`)

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"client_event_id", "started_at", "duration_ms", "avg_db", "session_id", "received_at",
	}); err != nil {
		h.deps.Logger.Error("csv header", "err", err.Error())
		return
	}

	emitted := 0
	cursor := store.Cursor{ReceivedAt: since, ID: uuid.Nil}
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		rows, err := h.deps.Store.ListEvents(r.Context(), uid, cursor, want)
		if err != nil {
			h.deps.Logger.Error("export csv", "err", err.Error())
			return // headers already written; can't switch to JSON error
		}
		if len(rows) == 0 {
			break
		}
		for _, e := range rows {
			if err := cw.Write([]string{
				e.ClientEventID,
				e.StartedAt.UTC().Format(time.RFC3339Nano),
				strconv.Itoa(int(e.DurationMS)),
				strconv.FormatFloat(float64(e.AvgDB), 'f', 2, 32),
				e.SessionID,
				e.ReceivedAt.UTC().Format(time.RFC3339Nano),
			}); err != nil {
				h.deps.Logger.Error("csv row", "err", err.Error())
				return
			}
			emitted++
			cursor = store.Cursor{ReceivedAt: e.ReceivedAt, ID: e.ID}
		}
		if max > 0 && emitted >= max {
			break
		}
		if len(rows) < want {
			break
		}
	}
	cw.Flush()
}

type jsonEvent struct {
	ClientEventID string    `json:"client_event_id"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int32     `json:"duration_ms"`
	AvgDB         float32   `json:"avg_db"`
	SessionID     string    `json:"session_id,omitempty"`
	ReceivedAt    time.Time `json:"received_at"`
}

// On store error mid-stream the response is truncated: the client
// will receive HTTP 200 + a partial body that's missing the closing
// `]}` and the `count` field. Documented MVP behavior — clients
// SHOULD treat malformed trailing JSON as "this export was partial,
// retry later." Future-work: write a chunked-encoding error trailer
// or switch to NDJSON so per-row errors are localizable.
func (h *handler) json(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.checkBudgetAndMaybeReject(w, r, uid) {
		return
	}
	ww := wrapWriter(w)
	w = ww
	defer h.recordAudit(r, uid, "json", ww)

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	max, err := parseMax(r.URL.Query().Get("max"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="snoreguard-events.json"`)

	enc := json.NewEncoder(w)
	if _, err := w.Write([]byte(`{"events":[`)); err != nil {
		return
	}

	emitted := 0
	cursor := store.Cursor{ReceivedAt: since, ID: uuid.Nil}
	first := true
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		rows, err := h.deps.Store.ListEvents(r.Context(), uid, cursor, want)
		if err != nil {
			h.deps.Logger.Error("export json", "err", err.Error())
			return
		}
		if len(rows) == 0 {
			break
		}
		for _, e := range rows {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return
				}
			}
			first = false
			// Encoder.Encode appends a newline; we don't want that
			// inside the array. Marshal manually.
			if err := enc.Encode(jsonEvent{
				ClientEventID: e.ClientEventID,
				StartedAt:     e.StartedAt.UTC(),
				DurationMS:    e.DurationMS,
				AvgDB:         e.AvgDB,
				SessionID:     e.SessionID,
				ReceivedAt:    e.ReceivedAt.UTC(),
			}); err != nil {
				h.deps.Logger.Error("json row", "err", err.Error())
				return
			}
			emitted++
			cursor = store.Cursor{ReceivedAt: e.ReceivedAt, ID: e.ID}
		}
		if max > 0 && emitted >= max {
			break
		}
		if len(rows) < want {
			break
		}
	}
	_, _ = w.Write([]byte(fmt.Sprintf(`],"count":%d}`, emitted)))
}

func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.New("invalid 'since' (want RFC3339)")
	}
	return t, nil
}

func parseMax(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 1_000_000 {
		return 0, errors.New("invalid 'max' (0..1000000)")
	}
	return n, nil
}

// checkBudgetAndMaybeReject enforces the per-user 24h export cap.
// Returns true if the response has already been written (429) and
// the caller should bail. Fail-open on store errors so a transient
// DB blip doesn't lock users out of their own data.
func (h *handler) checkBudgetAndMaybeReject(w http.ResponseWriter, r *http.Request, uid uuid.UUID) bool {
	if h.deps.Budget <= 0 {
		return false
	}
	ctx := r.Context()
	cutoff := h.deps.Now().Add(-h.deps.Window)
	count, err := h.deps.Store.CountExportsInWindow(ctx, uid, cutoff)
	if err != nil {
		h.deps.Logger.Warn("export budget count failed; failing open", "err", err.Error(), "user", uid.String())
		return false
	}
	if count < h.deps.Budget {
		return false
	}

	// Compute when the budget will reclaim a slot — the oldest
	// in-window row's age + window length.
	var retryAfter int64 = int64(h.deps.Window.Seconds())
	if oldest, err := h.deps.Store.OldestExportInWindow(ctx, uid, cutoff); err == nil && !oldest.IsZero() {
		secs := oldest.Add(h.deps.Window).Sub(h.deps.Now()).Seconds()
		if secs > 0 {
			retryAfter = int64(math.Ceil(secs))
		} else {
			retryAfter = 1
		}
	}
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	httpkit.JSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               fmt.Sprintf("export budget exceeded; retry in %ds", retryAfter),
		"retry_after_seconds": retryAfter,
	})
	return true
}

// auditWriter wraps an http.ResponseWriter so we can record the
// final status code + bytes streamed in the export_audit table.
type auditWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func wrapWriter(w http.ResponseWriter) *auditWriter {
	return &auditWriter{ResponseWriter: w, status: http.StatusOK}
}

func (a *auditWriter) WriteHeader(code int) {
	a.status = code
	a.ResponseWriter.WriteHeader(code)
}

func (a *auditWriter) Write(b []byte) (int, error) {
	n, err := a.ResponseWriter.Write(b)
	a.bytes += int64(n)
	return n, err
}

// recordAudit inserts an export_audit row capturing format, byte
// count, and final status. Always runs via defer so partial-stream
// errors still leave a paper trail. Uses a fresh background context
// with a short timeout because the request context may already be
// done by the time defer fires.
func (h *handler) recordAudit(r *http.Request, uid uuid.UUID, format string, ww *auditWriter) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var bytes *int64
	if ww.bytes > 0 {
		b := ww.bytes
		bytes = &b
	}
	if err := h.deps.Store.RecordExport(ctx, uid, format, bytes, ww.status); err != nil {
		h.deps.Logger.Error("record export audit",
			"err", err.Error(), "user", uid.String(),
			"format", format, "status", ww.status)
	}
}
