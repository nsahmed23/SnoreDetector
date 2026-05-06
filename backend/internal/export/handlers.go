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
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
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

const tracerName = "snoreguard/export-service"

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

	// Phase B: include=sessions,clips support.
	ListSessions(ctx context.Context, userID uuid.UUID, cursor store.DescCursor, limit int) ([]store.RecordingSession, error)
	ListAudioClips(ctx context.Context, userID uuid.UUID, cursor store.DescCursor, limit int) ([]store.AudioClip, error)
}

type Deps struct {
	Logger         *slog.Logger
	Store          Store
	JWT            *authjwt.Issuer
	RequestTimeout time.Duration

	// Metrics is the service's instrument bundle. Nil is fine.
	Metrics *metrics.Instruments

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
	r.Use(d.Metrics.HandlerDurationMiddleware)

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
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "export.events.csv",
		trace.WithAttributes(
			attribute.String("endpoint", "export.events.csv"),
			semconv.HTTPRoute("/export/events.csv"),
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

	if h.checkBudgetAndMaybeReject(w, r, uid) {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusTooManyRequests))
		return
	}
	ww := wrapWriter(w)
	w = ww
	defer h.recordAudit(r, uid, "csv", ww)

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	max, err := parseMax(r.URL.Query().Get("max"))
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	include, err := parseInclude(r.URL.Query().Get("include"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="snoreguard-events.csv"`)

	cw := csv.NewWriter(w)
	// Section header so spreadsheet importers / scripts can locate the
	// events section even when sessions/clips sections precede it.
	if include.Any() {
		if _, err := io.WriteString(w, "# section: events\n"); err != nil {
			return
		}
	}
	if err := cw.Write([]string{
		"client_event_id", "started_at", "duration_ms", "avg_db", "session_id", "received_at",
	}); err != nil {
		h.deps.Logger.Error("csv header", "err", err.Error())
		return
	}

	_, streamSpan := otel.Tracer(tracerName).Start(ctx, "export.events.csv.stream")
	emitted := 0
	cursor := store.Cursor{ReceivedAt: since, ID: uuid.Nil}
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		rows, err := h.deps.Store.ListEvents(ctx, uid, cursor, want)
		if err != nil {
			h.deps.Logger.Error("export csv", "err", err.Error())
			streamSpan.SetAttributes(attribute.Int("emitted", emitted))
			streamSpan.End()
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
				streamSpan.SetAttributes(attribute.Int("emitted", emitted))
				streamSpan.End()
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
	streamSpan.SetAttributes(attribute.Int("emitted", emitted))
	streamSpan.End()

	if include.Sessions {
		if err := h.csvSessions(r.Context(), w, cw, uid, max); err != nil {
			h.deps.Logger.Error("export csv sessions", "err", err.Error())
			return
		}
	}
	if include.Clips {
		if err := h.csvClips(r.Context(), w, cw, uid, max); err != nil {
			h.deps.Logger.Error("export csv clips", "err", err.Error())
			return
		}
	}

	// Counter increment happens after the stream completes — events
	// that made it into the body are what we count, regardless of
	// whether ww.bytes is later observed > 0 by recordAudit.
	if ww.status == http.StatusOK && ww.bytes > 0 {
		h.deps.Metrics.AddEventsExported(ctx, int64(emitted), metrics.FormatCSV)
	}
	span.SetAttributes(
		attribute.Int("emitted", emitted),
		semconv.HTTPResponseStatusCode(ww.status),
	)
}

// csvSessions writes the recording-sessions section after a blank
// line separator. Pages through ListSessions with the same DESC
// compound cursor used by the live API.
func (h *handler) csvSessions(ctx context.Context, w http.ResponseWriter, cw *csv.Writer, uid uuid.UUID, max int) error {
	if _, err := io.WriteString(w, "\n# section: sessions\n"); err != nil {
		return err
	}
	if err := cw.Write([]string{
		"session_id", "client_session_id", "started_at", "ended_at", "device_name", "app_version",
	}); err != nil {
		return err
	}
	emitted := 0
	cursor := store.DescCursor{}
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		if want <= 0 {
			break
		}
		rows, err := h.deps.Store.ListSessions(ctx, uid, cursor, want)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, s := range rows {
			ended := ""
			if s.EndedAt != nil {
				ended = s.EndedAt.UTC().Format(time.RFC3339Nano)
			}
			if err := cw.Write([]string{
				s.ID.String(),
				s.ClientSessionID,
				s.StartedAt.UTC().Format(time.RFC3339Nano),
				ended,
				s.DeviceName,
				s.AppVersion,
			}); err != nil {
				return err
			}
			emitted++
			cursor = store.DescCursor{StartedAt: s.StartedAt, ID: s.ID}
		}
		if len(rows) < want {
			break
		}
	}
	cw.Flush()
	return nil
}

// csvClips mirrors csvSessions for the audio_clips table. avg_db is
// nullable so we emit "" for nil rather than "0" (which would lie).
func (h *handler) csvClips(ctx context.Context, w http.ResponseWriter, cw *csv.Writer, uid uuid.UUID, max int) error {
	if _, err := io.WriteString(w, "\n# section: clips\n"); err != nil {
		return err
	}
	if err := cw.Write([]string{
		"clip_id", "session_id", "client_clip_id", "client_event_id",
		"started_at", "duration_ms", "avg_db", "content_type",
		"size_bytes", "sha256", "uploaded_at",
	}); err != nil {
		return err
	}
	emitted := 0
	cursor := store.DescCursor{}
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		if want <= 0 {
			break
		}
		rows, err := h.deps.Store.ListAudioClips(ctx, uid, cursor, want)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, c := range rows {
			sessionID := ""
			if c.SessionID != nil {
				sessionID = c.SessionID.String()
			}
			avgDB := ""
			if c.AvgDB != nil {
				avgDB = strconv.FormatFloat(float64(*c.AvgDB), 'f', 2, 32)
			}
			if err := cw.Write([]string{
				c.ID.String(),
				sessionID,
				c.ClientClipID,
				c.ClientEventID,
				c.StartedAt.UTC().Format(time.RFC3339Nano),
				strconv.Itoa(int(c.DurationMS)),
				avgDB,
				c.ContentType,
				strconv.FormatInt(c.SizeBytes, 10),
				c.SHA256,
				c.UploadedAt.UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			emitted++
			cursor = store.DescCursor{StartedAt: c.StartedAt, ID: c.ID}
		}
		if len(rows) < want {
			break
		}
	}
	cw.Flush()
	return nil
}

type jsonEvent struct {
	ClientEventID string    `json:"client_event_id"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int32     `json:"duration_ms"`
	AvgDB         float32   `json:"avg_db"`
	SessionID     string    `json:"session_id,omitempty"`
	ReceivedAt    time.Time `json:"received_at"`
}

// jsonSession + jsonClip are the export-side DTOs for the
// include=sessions,clips top-level shape. Same fields the live API
// returns; `,omitempty` for nullable fields preserves wire compactness.
type jsonSession struct {
	SessionID       string     `json:"session_id"`
	ClientSessionID string     `json:"client_session_id"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DeviceName      string     `json:"device_name,omitempty"`
	AppVersion      string     `json:"app_version,omitempty"`
}

type jsonClip struct {
	ClipID        string    `json:"clip_id"`
	SessionID     string    `json:"session_id,omitempty"`
	ClientClipID  string    `json:"client_clip_id"`
	ClientEventID string    `json:"client_event_id,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int32     `json:"duration_ms"`
	AvgDB         *float32  `json:"avg_db,omitempty"`
	ContentType   string    `json:"content_type"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	UploadedAt    time.Time `json:"uploaded_at"`
}

// On store error mid-stream the response is truncated: the client
// will receive HTTP 200 + a partial body that's missing the closing
// `]}` and the `count` field. Documented MVP behavior — clients
// SHOULD treat malformed trailing JSON as "this export was partial,
// retry later." Future-work: write a chunked-encoding error trailer
// or switch to NDJSON so per-row errors are localizable.
//
// When include=sessions,clips is set we change the top-level shape:
//
//	{"events":[…], "sessions":[…], "clips":[…],
//	 "count": {"events": N, "sessions": M, "clips": K}}
//
// versus the original `{"events":[…], "count": N}`. Backward compat
// kicks in whenever the include set is empty.
func (h *handler) json(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "export.events.json",
		trace.WithAttributes(
			attribute.String("endpoint", "export.events.json"),
			semconv.HTTPRoute("/export/events.json"),
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

	if h.checkBudgetAndMaybeReject(w, r, uid) {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusTooManyRequests))
		return
	}
	ww := wrapWriter(w)
	w = ww
	defer h.recordAudit(r, uid, "json", ww)

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	max, err := parseMax(r.URL.Query().Get("max"))
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	include, err := parseInclude(r.URL.Query().Get("include"))
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

	_, streamSpan := otel.Tracer(tracerName).Start(ctx, "export.events.json.stream")
	emittedEvents := 0
	cursor := store.Cursor{ReceivedAt: since, ID: uuid.Nil}
	first := true
	for {
		want := pageSize
		if max > 0 && max-emittedEvents < want {
			want = max - emittedEvents
		}
		rows, err := h.deps.Store.ListEvents(ctx, uid, cursor, want)
		if err != nil {
			h.deps.Logger.Error("export json", "err", err.Error())
			streamSpan.SetAttributes(attribute.Int("emitted", emittedEvents))
			streamSpan.End()
			return
		}
		if len(rows) == 0 {
			break
		}
		for _, e := range rows {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					streamSpan.SetAttributes(attribute.Int("emitted", emittedEvents))
					streamSpan.End()
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
				streamSpan.SetAttributes(attribute.Int("emitted", emittedEvents))
				streamSpan.End()
				return
			}
			emittedEvents++
			cursor = store.Cursor{ReceivedAt: e.ReceivedAt, ID: e.ID}
		}
		if max > 0 && emittedEvents >= max {
			break
		}
		if len(rows) < want {
			break
		}
	}

	if !include.Any() {
		// Original shape: {"events":[…],"count":N}.
		_, _ = w.Write([]byte(fmt.Sprintf(`],"count":%d}`, emittedEvents)))
	streamSpan.SetAttributes(attribute.Int("emitted", emittedEvents))
	streamSpan.End()
	if ww.status == http.StatusOK && ww.bytes > 0 {
		h.deps.Metrics.AddEventsExported(ctx, int64(emittedEvents), metrics.FormatJSON)
	}
	span.SetAttributes(
		attribute.Int("emitted", emittedEvents),
		semconv.HTTPResponseStatusCode(ww.status),
	)
		return
	}
	// Extended shape: write sessions[], clips[], then the count map.
	if _, err := w.Write([]byte(`]`)); err != nil {
		return
	}
	emittedSessions := 0
	if include.Sessions {
		n, err := h.jsonSessions(r.Context(), w, enc, uid, max)
		if err != nil {
			h.deps.Logger.Error("export json sessions", "err", err.Error())
			return
		}
		emittedSessions = n
	}
	emittedClips := 0
	if include.Clips {
		n, err := h.jsonClips(r.Context(), w, enc, uid, max)
		if err != nil {
			h.deps.Logger.Error("export json clips", "err", err.Error())
			return
		}
		emittedClips = n
	}
	count := map[string]int{
		"events": emittedEvents,
	}
	if include.Sessions {
		count["sessions"] = emittedSessions
	}
	if include.Clips {
		count["clips"] = emittedClips
	}
	cb, _ := json.Marshal(count)
	_, _ = w.Write([]byte(`,"count":`))
	_, _ = w.Write(cb)
	_, _ = w.Write([]byte(`}`))
	streamSpan.SetAttributes(attribute.Int("emitted", emittedEvents+emittedSessions+emittedClips))
	streamSpan.End()
	if ww.status == http.StatusOK && ww.bytes > 0 {
		h.deps.Metrics.AddEventsExported(ctx, int64(emittedEvents+emittedSessions+emittedClips), metrics.FormatJSON)
	}
	span.SetAttributes(
		attribute.Int("emitted", emittedEvents+emittedSessions+emittedClips),
		semconv.HTTPResponseStatusCode(ww.status),
	)
}

func (h *handler) jsonSessions(ctx context.Context, w http.ResponseWriter, enc *json.Encoder, uid uuid.UUID, max int) (int, error) {
	if _, err := w.Write([]byte(`,"sessions":[`)); err != nil {
		return 0, err
	}
	emitted := 0
	first := true
	cursor := store.DescCursor{}
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		if want <= 0 {
			break
		}
		rows, err := h.deps.Store.ListSessions(ctx, uid, cursor, want)
		if err != nil {
			return emitted, err
		}
		if len(rows) == 0 {
			break
		}
		for _, s := range rows {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return emitted, err
				}
			}
			first = false
			if err := enc.Encode(jsonSession{
				SessionID:       s.ID.String(),
				ClientSessionID: s.ClientSessionID,
				StartedAt:       s.StartedAt.UTC(),
				EndedAt:         s.EndedAt,
				DeviceName:      s.DeviceName,
				AppVersion:      s.AppVersion,
			}); err != nil {
				return emitted, err
			}
			emitted++
			cursor = store.DescCursor{StartedAt: s.StartedAt, ID: s.ID}
		}
		if len(rows) < want {
			break
		}
	}
	if _, err := w.Write([]byte(`]`)); err != nil {
		return emitted, err
	}
	return emitted, nil
}

func (h *handler) jsonClips(ctx context.Context, w http.ResponseWriter, enc *json.Encoder, uid uuid.UUID, max int) (int, error) {
	if _, err := w.Write([]byte(`,"clips":[`)); err != nil {
		return 0, err
	}
	emitted := 0
	first := true
	cursor := store.DescCursor{}
	for {
		want := pageSize
		if max > 0 && max-emitted < want {
			want = max - emitted
		}
		if want <= 0 {
			break
		}
		rows, err := h.deps.Store.ListAudioClips(ctx, uid, cursor, want)
		if err != nil {
			return emitted, err
		}
		if len(rows) == 0 {
			break
		}
		for _, c := range rows {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return emitted, err
				}
			}
			first = false
			d := jsonClip{
				ClipID:        c.ID.String(),
				ClientClipID:  c.ClientClipID,
				ClientEventID: c.ClientEventID,
				StartedAt:     c.StartedAt.UTC(),
				DurationMS:    c.DurationMS,
				AvgDB:         c.AvgDB,
				ContentType:   c.ContentType,
				SizeBytes:     c.SizeBytes,
				SHA256:        c.SHA256,
				UploadedAt:    c.UploadedAt.UTC(),
			}
			if c.SessionID != nil {
				d.SessionID = c.SessionID.String()
			}
			if err := enc.Encode(d); err != nil {
				return emitted, err
			}
			emitted++
			cursor = store.DescCursor{StartedAt: c.StartedAt, ID: c.ID}
		}
		if len(rows) < want {
			break
		}
	}
	if _, err := w.Write([]byte(`]`)); err != nil {
		return emitted, err
	}
	return emitted, nil
}

// includeSet describes which extra sections the export should include.
// Empty == backward-compat (events only, original wire shape).
type includeSet struct {
	Sessions bool
	Clips    bool
}

func (i includeSet) Any() bool { return i.Sessions || i.Clips }

func parseInclude(s string) (includeSet, error) {
	out := includeSet{}
	if s == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		switch strings.TrimSpace(part) {
		case "sessions":
			out.Sessions = true
		case "clips":
			out.Clips = true
		case "":
			// "include=,sessions" tolerated; ignore empty parts.
		default:
			return includeSet{}, errors.New("invalid 'include' (want comma-separated subset of: sessions,clips)")
		}
	}
	return out, nil
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
