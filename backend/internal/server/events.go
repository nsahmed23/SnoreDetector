package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const (
	maxEventsPerRequest = 500
	maxRequestBytes     = 1 << 20 // 1 MiB
)

type eventsHandler struct {
	deps Deps
}

type eventDTO struct {
	ClientEventID string    `json:"client_event_id"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int32     `json:"duration_ms"`
	AvgDB         float32   `json:"avg_db"`
	SessionID     string    `json:"session_id,omitempty"`
}

type createEventsRequest struct {
	Events []eventDTO `json:"events"`
}

type createEventsResponse struct {
	Inserted int `json:"inserted"`
	Received int `json:"received"`
}

func (h *eventsHandler) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req createEventsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "events array is empty")
		return
	}
	if len(req.Events) > maxEventsPerRequest {
		writeError(w, http.StatusBadRequest, "too many events in one request")
		return
	}

	// All-or-nothing: any malformed event rejects the whole batch.
	// This keeps import semantics atomic — clients never have to
	// reason about partial success.
	rows := make([]store.SnoreEvent, 0, len(req.Events))
	for i, e := range req.Events {
		if err := validateEvent(e); err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("event[%d]: %v", i, err))
			return
		}
		rows = append(rows, store.SnoreEvent{
			UserID:        uid,
			ClientEventID: e.ClientEventID,
			StartedAt:     e.StartedAt,
			DurationMS:    e.DurationMS,
			AvgDB:         e.AvgDB,
			SessionID:     e.SessionID,
		})
	}

	inserted, err := h.deps.Store.InsertEvents(r.Context(), uid, rows)
	if err != nil {
		h.deps.Logger.Error("insert events", "err", err.Error(), "user", uid.String())
		writeError(w, http.StatusInternalServerError, "failed to record events")
		return
	}

	writeJSON(w, http.StatusOK, createEventsResponse{
		Inserted: inserted,
		Received: len(req.Events),
	})
}

type listEventsResponse struct {
	Events []eventDTO `json:"events"`
	// NextCursor is an opaque base64 cursor pointing at the last row
	// returned. Pass it back as `?cursor=` for the next page. Omitted
	// when there are no more rows to return.
	NextCursor string `json:"next_cursor,omitempty"`
}

func (h *eventsHandler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	q := r.URL.Query()
	cursor, err := resolveCursor(q.Get("cursor"), q.Get("since"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := 200
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, "invalid 'limit' (1..1000)")
			return
		}
		limit = n
	}

	rows, err := h.deps.Store.ListEvents(r.Context(), uid, cursor, limit)
	if err != nil {
		h.deps.Logger.Error("list events", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to list events")
		return
	}

	out := make([]eventDTO, 0, len(rows))
	for _, e := range rows {
		out = append(out, eventDTO{
			ClientEventID: e.ClientEventID,
			StartedAt:     e.StartedAt,
			DurationMS:    e.DurationMS,
			AvgDB:         e.AvgDB,
			SessionID:     e.SessionID,
		})
	}
	resp := listEventsResponse{Events: out}
	// Only emit a next_cursor when this page was full — otherwise the
	// caller has reached the end and shouldn't paginate further.
	if len(rows) == limit && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeCursor(store.Cursor{ReceivedAt: last.ReceivedAt, ID: last.ID})
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveCursor picks between the opaque `cursor` param and the legacy
// `since` param. `cursor` wins when both are supplied. Empty inputs
// yield the zero cursor (start from the beginning).
func resolveCursor(cursorParam, sinceParam string) (store.Cursor, error) {
	if cursorParam != "" {
		c, err := decodeCursor(cursorParam)
		if err != nil {
			return store.Cursor{}, errors.New("invalid 'cursor' parameter")
		}
		return c, nil
	}
	if sinceParam != "" {
		t, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			return store.Cursor{}, errors.New("invalid 'since' parameter (want RFC3339)")
		}
		return store.Cursor{ReceivedAt: t.UTC()}, nil
	}
	return store.ZeroCursor(), nil
}

func validateEvent(e eventDTO) error {
	if e.ClientEventID == "" {
		return errors.New("client_event_id is required")
	}
	if len(e.ClientEventID) > 128 {
		return errors.New("client_event_id too long")
	}
	if e.StartedAt.IsZero() {
		return errors.New("started_at is required")
	}
	if e.DurationMS < 0 {
		return errors.New("duration_ms must be ≥ 0")
	}
	if e.DurationMS > int32(24*time.Hour/time.Millisecond) {
		return errors.New("duration_ms unreasonably large")
	}
	avg := float64(e.AvgDB)
	if math.IsNaN(avg) || math.IsInf(avg, 0) {
		return errors.New("avg_db must be finite")
	}
	if avg < 0 || avg > 200 {
		return errors.New("avg_db out of range [0, 200]")
	}
	return nil
}
