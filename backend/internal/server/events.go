package server

import (
	"encoding/json"
	"errors"
	"fmt"
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
	// NextCursor is the `received_at` value of the last returned
	// event in RFC3339Nano. The client passes it back as ?since= for
	// the next page.
	NextCursor string `json:"next_cursor,omitempty"`
}

func (h *eventsHandler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := UserIDFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid 'since' parameter (want RFC3339)")
		return
	}
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 || n > 1000 {
			writeError(w, http.StatusBadRequest, "invalid 'limit' (1..1000)")
			return
		}
		limit = n
	}

	rows, err := h.deps.Store.ListEvents(r.Context(), uid, since, limit)
	if err != nil {
		h.deps.Logger.Error("list events", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to list events")
		return
	}

	out := make([]eventDTO, 0, len(rows))
	var lastReceived time.Time
	for _, e := range rows {
		out = append(out, eventDTO{
			ClientEventID: e.ClientEventID,
			StartedAt:     e.StartedAt,
			DurationMS:    e.DurationMS,
			AvgDB:         e.AvgDB,
			SessionID:     e.SessionID,
		})
		lastReceived = e.ReceivedAt
	}
	resp := listEventsResponse{Events: out}
	if !lastReceived.IsZero() {
		resp.NextCursor = lastReceived.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, resp)
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
	return nil
}

func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

