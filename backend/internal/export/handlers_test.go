package export

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const testKey = "test-signing-key-must-be-at-least-32-bytes-long!"

// fakeStore returns events page-by-page (after `since`) so we can
// exercise the streaming pagination path. Only the user the test
// authenticates as gets results — everything else is filtered.
type fakeStore struct {
	events  []store.SnoreEvent
	pingErr error
	listErr error
}

func (f *fakeStore) ListEvents(_ context.Context, uid uuid.UUID, cursor store.Cursor, limit int) ([]store.SnoreEvent, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	// Mirror the SQL: filter by user, apply compound cursor `(received_at, id) > cursor`,
	// and order by `received_at ASC, id ASC` before slicing.
	candidates := make([]store.SnoreEvent, 0)
	for _, e := range f.events {
		if e.UserID != uid {
			continue
		}
		if e.ReceivedAt.Before(cursor.ReceivedAt) {
			continue
		}
		if e.ReceivedAt.Equal(cursor.ReceivedAt) {
			// Compare UUIDs lexicographically — matches Postgres uuid ordering.
			if e.ID.String() <= cursor.ID.String() {
				continue
			}
		}
		candidates = append(candidates, e)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].ReceivedAt.Equal(candidates[j].ReceivedAt) {
			return candidates[i].ReceivedAt.Before(candidates[j].ReceivedAt)
		}
		return candidates[i].ID.String() < candidates[j].ID.String()
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}
func (f *fakeStore) Ping(_ context.Context) error                           { return f.pingErr }
func (f *fakeStore) IsJTIRevoked(_ context.Context, _ string) (bool, error) { return false, nil }

type harness struct {
	router http.Handler
	store  *fakeStore
	jwt    *authjwt.Issuer
	uid    uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	iss, err := authjwt.New([]byte(testKey), "test-iss", time.Hour)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	st := &fakeStore{}
	uid := uuid.New()
	return &harness{
		router: New(Deps{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Store:  st,
			JWT:    iss,
		}),
		store: st,
		jwt:   iss,
		uid:   uid,
	}
}

func (h *harness) authReq(t *testing.T, target string) *http.Request {
	t.Helper()
	tok, err := h.jwt.Issue(h.uid)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	return r
}

func seed(h *harness, count int) {
	base := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		h.store.events = append(h.store.events, store.SnoreEvent{
			ID:            uuid.New(),
			UserID:        h.uid,
			ClientEventID: uuid.NewString(),
			StartedAt:     base.Add(time.Duration(i) * time.Minute),
			DurationMS:    int32(1000 + i),
			AvgDB:         60.5,
			SessionID:     "sess-1",
			ReceivedAt:    base.Add(time.Duration(i) * time.Minute),
		})
	}
}

func TestExportCSV_Streams(t *testing.T) {
	h := newHarness(t)
	seed(h, 3)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content-type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "snoreguard-events.csv") {
		t.Errorf("content-disposition = %q", cd)
	}

	r := csv.NewReader(rec.Body)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("rows = %d, want 4 (header + 3)", len(rows))
	}
	if rows[0][0] != "client_event_id" {
		t.Errorf("missing csv header: %v", rows[0])
	}
}

func TestExportCSV_Empty(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	r := csv.NewReader(rec.Body)
	rows, _ := r.ReadAll()
	if len(rows) != 1 {
		t.Errorf("rows = %d, want 1 (header only)", len(rows))
	}
}

func TestExportCSV_RespectsMax(t *testing.T) {
	h := newHarness(t)
	seed(h, 10)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv?max=3"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	r := csv.NewReader(rec.Body)
	rows, _ := r.ReadAll()
	if len(rows) != 4 {
		t.Errorf("rows = %d, want 4 (header + 3)", len(rows))
	}
}

func TestExportCSV_OnlyOwnEvents(t *testing.T) {
	h := newHarness(t)
	seed(h, 2)
	// Add an event for a different user.
	h.store.events = append(h.store.events, store.SnoreEvent{
		UserID:        uuid.New(),
		ClientEventID: "stranger",
		StartedAt:     time.Now(),
		DurationMS:    100,
		AvgDB:         60,
		ReceivedAt:    time.Now().Add(time.Hour),
	})

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "stranger") {
		t.Error("export leaked another user's event")
	}
}

func TestExportCSV_RejectsBadSince(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv?since=blah"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestExportCSV_RejectsBadMax(t *testing.T) {
	h := newHarness(t)
	for _, m := range []string{"-1", "abc", "999999999"} {
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv?max="+m))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("max=%q: status = %d", m, rec.Code)
		}
	}
}

func TestExportCSV_RequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/export/events.csv", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestExportJSON_Streams(t *testing.T) {
	h := newHarness(t)
	seed(h, 3)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Events []map[string]any `json:"events"`
		Count  int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if resp.Count != 3 || len(resp.Events) != 3 {
		t.Errorf("count=%d, events=%d", resp.Count, len(resp.Events))
	}
}

func TestExportJSON_Empty(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Events []map[string]any `json:"events"`
		Count  int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if resp.Count != 0 || len(resp.Events) != 0 {
		t.Errorf("expected empty response, got count=%d, events=%d", resp.Count, len(resp.Events))
	}
}

func TestExportCSV_HandlesPaginationAcrossSharedReceivedAt(t *testing.T) {
	h := newHarness(t)
	// Seed 250 events, all sharing a single received_at to stress the cursor.
	sharedReceivedAt := time.Date(2026, 5, 5, 4, 0, 0, 0, time.UTC)
	base := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 250; i++ {
		h.store.events = append(h.store.events, store.SnoreEvent{
			ID:            uuid.New(),
			UserID:        h.uid,
			ClientEventID: fmt.Sprintf("cursor-test-%03d", i),
			StartedAt:     base.Add(time.Duration(i) * time.Second),
			DurationMS:    1000,
			AvgDB:         60,
			ReceivedAt:    sharedReceivedAt, // <-- all the same
		})
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	r := csv.NewReader(rec.Body)
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	// 250 rows + 1 header
	if len(rows) != 251 {
		t.Errorf("rows = %d, want 251 (header + 250)", len(rows))
	}
	// Confirm all unique
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		if seen[row[0]] {
			t.Errorf("duplicate client_event_id: %s", row[0])
		}
		seen[row[0]] = true
	}
}

func TestExportJSON_StoreError_500AfterHeaders(t *testing.T) {
	// Once headers are sent we can't change status; we just stop
	// writing rows. This test confirms we don't panic.
	h := newHarness(t)
	h.store.listErr = errors.New("db gone")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json"))
	// 200 because the error happens after we've already written `{"events":[`.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}
