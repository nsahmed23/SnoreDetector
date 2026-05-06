package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

func TestPostEvents_RejectsAvgDB(t *testing.T) {
	cases := []struct {
		name  string
		avgDB any // allow non-finite via json.Number / explicit string
	}{
		{"NaN", "NaN"},
		{"Inf", "Inf"},
		{"NegInf", "-Inf"},
		{"negative", -1},
		{"too_large", 999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tok := h.sessionToken(t, uuid.New())
			// JSON spec doesn't allow NaN/Inf literals, so we
			// hand-craft the JSON for those cases.
			var raw string
			switch v := tc.avgDB.(type) {
			case string:
				raw = fmt.Sprintf(`{"events":[{"client_event_id":"x","started_at":"2026-05-05T03:14:00Z","duration_ms":1000,"avg_db":%s}]}`, v)
			default:
				raw = fmt.Sprintf(`{"events":[{"client_event_id":"x","started_at":"2026-05-05T03:14:00Z","duration_ms":1000,"avg_db":%v}]}`, v)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader([]byte(raw)))
			req.Header.Set("Authorization", "Bearer "+tok)
			h.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPostEvents_AcceptsValidAvgDB(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	for _, v := range []float32{0, 0.5, 60, 200} {
		body := map[string]any{"events": []map[string]any{{
			"client_event_id": fmt.Sprintf("ok-%v", v),
			"started_at":      "2026-05-05T03:14:00Z",
			"duration_ms":     1000,
			"avg_db":          v,
		}}}
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+tok)
		h.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("avg_db=%v: status=%d body=%s, want 200", v, rec.Code, rec.Body.String())
		}
	}
}

// TestGetEvents_PaginatesCompoundCursor exercises the new opaque
// cursor against the in-memory fake. It seeds events that share a
// received_at to the nanosecond and confirms the second page picks up
// where the first left off.
func TestGetEvents_PaginatesCompoundCursor(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)

	// Seed 5 events with identical ReceivedAt and unique IDs.
	rxAt := time.Date(2026, 5, 5, 3, 14, 0, 0, time.UTC)
	ids := []uuid.UUID{}
	for i := 0; i < 5; i++ {
		id := uuid.New()
		ids = append(ids, id)
		h.store.events = append(h.store.events, store.SnoreEvent{
			ID:            id,
			UserID:        uid,
			ClientEventID: fmt.Sprintf("ev-%d", i),
			StartedAt:     time.Now(),
			DurationMS:    1000,
			AvgDB:         60,
			ReceivedAt:    rxAt,
		})
	}

	get := func(cursor string) listEventsResponse {
		t.Helper()
		url := "/events?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		h.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp listEventsResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 5; page++ {
		resp := get(cursor)
		for _, e := range resp.Events {
			if seen[e.ClientEventID] {
				t.Fatalf("duplicate %s on page %d", e.ClientEventID, page)
			}
			seen[e.ClientEventID] = true
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("paginated %d events, want 5", len(seen))
	}
}

func TestGetEvents_AcceptsLegacySinceParam(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	now := time.Date(2026, 5, 5, 3, 14, 0, 0, time.UTC)
	h.store.events = []store.SnoreEvent{
		{ID: uuid.New(), UserID: uid, ClientEventID: "old", StartedAt: now, DurationMS: 1, AvgDB: 60, ReceivedAt: now.Add(-1 * time.Hour)},
		{ID: uuid.New(), UserID: uid, ClientEventID: "new", StartedAt: now, DurationMS: 1, AvgDB: 60, ReceivedAt: now.Add(time.Hour)},
	}
	rec := httptest.NewRecorder()
	since := now.UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/events?since="+since, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp listEventsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Events) != 1 || resp.Events[0].ClientEventID != "new" {
		t.Errorf("legacy since= filter broken: %+v", resp.Events)
	}
}

func TestGetEvents_RejectsBadCursor(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?cursor=not-base64-or-anything", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestEncodeDecodeCursor_RoundTrip(t *testing.T) {
	c := store.Cursor{ReceivedAt: time.Now().UTC().Truncate(time.Nanosecond), ID: uuid.New()}
	enc := encodeCursor(c)
	got, err := decodeCursor(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReceivedAt.Equal(c.ReceivedAt) || got.ID != c.ID {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, c)
	}
}
