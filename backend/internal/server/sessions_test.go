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

func postSessionAs(t *testing.T, h *harness, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func TestPostSession_Creates(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	rec := postSessionAs(t, h, tok, map[string]any{
		"client_session_id": "client-sess-1",
		"started_at":        "2026-05-05T03:14:00Z",
		"device_name":       "iPhone 16",
		"app_version":       "1.0.0",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp postSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Created {
		t.Errorf("created=false on first POST: %+v", resp)
	}
	if _, err := uuid.Parse(resp.SessionID); err != nil {
		t.Errorf("session_id not a UUID: %q", resp.SessionID)
	}
}

func TestPostSession_IdempotentOnClientSessionID(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	body := map[string]any{
		"client_session_id": "dup-session",
		"started_at":        "2026-05-05T03:14:00Z",
	}
	first := postSessionAs(t, h, tok, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first: status=%d body=%s", first.Code, first.Body.String())
	}
	var firstResp postSessionResponse
	_ = json.Unmarshal(first.Body.Bytes(), &firstResp)

	second := postSessionAs(t, h, tok, body)
	if second.Code != http.StatusOK {
		t.Fatalf("second: status=%d body=%s", second.Code, second.Body.String())
	}
	var secondResp postSessionResponse
	_ = json.Unmarshal(second.Body.Bytes(), &secondResp)

	if secondResp.Created {
		t.Errorf("second POST returned created=true; idempotency broken")
	}
	if secondResp.SessionID != firstResp.SessionID {
		t.Errorf("session_id changed: %q -> %q", firstResp.SessionID, secondResp.SessionID)
	}
}

func TestPostSession_RejectsMissingFields(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	cases := []map[string]any{
		{"started_at": "2026-05-05T03:14:00Z"},                                                     // missing client_session_id
		{"client_session_id": "x"},                                                                  // missing started_at
		{"client_session_id": "x", "started_at": "2026-05-05T03:14:00Z", "ended_at": "2026-05-04T03:14:00Z"}, // ended < started
	}
	for i, body := range cases {
		rec := postSessionAs(t, h, tok, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: status=%d body=%s, want 400", i, rec.Code, rec.Body.String())
		}
	}
}

func TestGetSessions_DescPaginate(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	// Seed 5 sessions with monotonically increasing started_at; we
	// expect them back newest-first.
	base := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	want := []string{}
	for i := 0; i < 5; i++ {
		csid := fmt.Sprintf("sess-%02d", i)
		h.store.sessions = append(h.store.sessions, store.RecordingSession{
			ID:              uuid.New(),
			UserID:          uid,
			ClientSessionID: csid,
			StartedAt:       base.Add(time.Duration(i) * time.Hour),
		})
		want = append([]string{csid}, want...) // prepend (DESC)
	}

	get := func(cursor string) listSessionsResponse {
		t.Helper()
		url := "/sessions?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp listSessionsResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}
	var seen []string
	cursor := ""
	for page := 0; page < 5; page++ {
		resp := get(cursor)
		for _, s := range resp.Sessions {
			seen = append(seen, s.ClientSessionID)
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("paginated %d, want 5: %v", len(seen), seen)
	}
	for i := range seen {
		if seen[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full=%v)", i, seen[i], want[i], seen)
		}
	}
}

func TestGetSessions_OnlyOwn(t *testing.T) {
	h := newHarness(t)
	mine := uuid.New()
	other := uuid.New()
	now := time.Now().UTC()
	h.store.sessions = []store.RecordingSession{
		{ID: uuid.New(), UserID: mine, ClientSessionID: "mine", StartedAt: now},
		{ID: uuid.New(), UserID: other, ClientSessionID: "other", StartedAt: now},
	}
	tok := h.sessionToken(t, mine)
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp listSessionsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Sessions) != 1 || resp.Sessions[0].ClientSessionID != "mine" {
		t.Errorf("returned wrong sessions: %+v", resp.Sessions)
	}
}

func TestPostSession_RequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(`{}`)))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", rec.Code)
	}
}
