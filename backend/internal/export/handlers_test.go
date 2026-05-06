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
	"sync"
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
	events   []store.SnoreEvent
	sessions []store.RecordingSession
	clips    []store.AudioClip
	pingErr  error
	listErr  error

	// Per-user export-audit log — populated by RecordExport, queried
	// by CountExportsInWindow / OldestExportInWindow.
	auditMu sync.Mutex
	audits  []exportAudit
}

type exportAudit struct {
	userID      uuid.UUID
	requestedAt time.Time
	format      string
	bytesSent   *int64
	statusCode  int
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

func (f *fakeStore) RecordExport(_ context.Context, uid uuid.UUID, format string, bytesSent *int64, status int) error {
	f.auditMu.Lock()
	defer f.auditMu.Unlock()
	f.audits = append(f.audits, exportAudit{
		userID:      uid,
		requestedAt: time.Now().UTC(),
		format:      format,
		bytesSent:   bytesSent,
		statusCode:  status,
	})
	return nil
}

func (f *fakeStore) CountExportsInWindow(_ context.Context, uid uuid.UUID, since time.Time) (int, error) {
	f.auditMu.Lock()
	defer f.auditMu.Unlock()
	count := 0
	for _, a := range f.audits {
		if a.userID == uid && a.requestedAt.After(since) {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) OldestExportInWindow(_ context.Context, uid uuid.UUID, since time.Time) (time.Time, error) {
	f.auditMu.Lock()
	defer f.auditMu.Unlock()
	var oldest time.Time
	for _, a := range f.audits {
		if a.userID != uid || !a.requestedAt.After(since) {
			continue
		}
		if oldest.IsZero() || a.requestedAt.Before(oldest) {
			oldest = a.requestedAt
		}
	}
	return oldest, nil
}

// ListSessions / ListAudioClips power the include=sessions,clips path.
// Both apply the same DESC compound-cursor filter as the real store.
func (f *fakeStore) ListSessions(_ context.Context, uid uuid.UUID, cursor store.DescCursor, limit int) ([]store.RecordingSession, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	mine := []store.RecordingSession{}
	for _, s := range f.sessions {
		if s.UserID != uid {
			continue
		}
		mine = append(mine, s)
	}
	sort.Slice(mine, func(i, j int) bool {
		if !mine[i].StartedAt.Equal(mine[j].StartedAt) {
			return mine[i].StartedAt.After(mine[j].StartedAt)
		}
		return mine[i].ID.String() > mine[j].ID.String()
	})
	out := []store.RecordingSession{}
	for _, s := range mine {
		if !cursor.StartedAt.IsZero() || cursor.ID != uuid.Nil {
			if !descLess(s.StartedAt, s.ID, cursor.StartedAt, cursor.ID) {
				continue
			}
		}
		out = append(out, s)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) ListAudioClips(_ context.Context, uid uuid.UUID, cursor store.DescCursor, limit int) ([]store.AudioClip, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	mine := []store.AudioClip{}
	for _, c := range f.clips {
		if c.UserID != uid || c.DeletedAt != nil {
			continue
		}
		mine = append(mine, c)
	}
	sort.Slice(mine, func(i, j int) bool {
		if !mine[i].StartedAt.Equal(mine[j].StartedAt) {
			return mine[i].StartedAt.After(mine[j].StartedAt)
		}
		return mine[i].ID.String() > mine[j].ID.String()
	})
	out := []store.AudioClip{}
	for _, c := range mine {
		if !cursor.StartedAt.IsZero() || cursor.ID != uuid.Nil {
			if !descLess(c.StartedAt, c.ID, cursor.StartedAt, cursor.ID) {
				continue
			}
		}
		out = append(out, c)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// descLess returns true iff (at, aid) < (bt, bid) lexicographically.
// Used to enforce "strictly less than the cursor" for DESC pagination.
func descLess(at time.Time, aid uuid.UUID, bt time.Time, bid uuid.UUID) bool {
	if at.Before(bt) {
		return true
	}
	if at.After(bt) {
		return false
	}
	return aid.String() < bid.String()
}

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

// budgetHarness builds a harness with a finite per-user export budget
// over a configurable rolling window. Used by the budget-enforcement
// tests below.
func budgetHarness(t *testing.T, budget int, window time.Duration) *harness {
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
			Budget: budget,
			Window: window,
		}),
		store: st,
		jwt:   iss,
		uid:   uid,
	}
}

func TestExportCSV_RespectsBudget(t *testing.T) {
	h := budgetHarness(t, 2, 24*time.Hour)
	seed(h, 3)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200", i+1, rec.Code)
		}
	}
	// Third request must hit the budget.
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third call: status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on budget rejection")
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["retry_after_seconds"]; !ok {
		t.Errorf("missing retry_after_seconds in body: %v", resp)
	}
}

func TestExportJSON_RespectsBudget(t *testing.T) {
	h := budgetHarness(t, 1, 24*time.Hour)
	seed(h, 2)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first call: status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second call: status = %d, want 429", rec.Code)
	}
}

func TestExportCSV_ZeroBudgetMeansUnlimited(t *testing.T) {
	// Budget=0 disables the budget check entirely.
	h := budgetHarness(t, 0, 24*time.Hour)
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d should not be limited (budget=0): status = %d", i+1, rec.Code)
		}
	}
}

func TestExportCSV_WritesAuditRow(t *testing.T) {
	h := budgetHarness(t, 100, 24*time.Hour)
	seed(h, 2)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(h.store.audits) != 1 {
		t.Fatalf("audits = %d, want 1", len(h.store.audits))
	}
	a := h.store.audits[0]
	if a.format != "csv" {
		t.Errorf("format = %q", a.format)
	}
	if a.statusCode != http.StatusOK {
		t.Errorf("statusCode = %d", a.statusCode)
	}
	if a.bytesSent == nil || *a.bytesSent == 0 {
		t.Errorf("bytesSent should be non-zero, got %v", a.bytesSent)
	}
}

// --- Phase B: ?include=sessions,clips coverage ---

func seedSession(h *harness, csid string, startedAt time.Time) uuid.UUID {
	id := uuid.New()
	h.store.sessions = append(h.store.sessions, store.RecordingSession{
		ID:              id,
		UserID:          h.uid,
		ClientSessionID: csid,
		StartedAt:       startedAt,
	})
	return id
}

func seedClip(h *harness, ccid string, startedAt time.Time) uuid.UUID {
	id := uuid.New()
	h.store.clips = append(h.store.clips, store.AudioClip{
		ID:           id,
		UserID:       h.uid,
		ClientClipID: ccid,
		StartedAt:    startedAt,
		DurationMS:   1500,
		ContentType:  "audio/m4a",
		SizeBytes:    2048,
		SHA256:       strings.Repeat("a", 64),
		ObjectKey:    "clips/" + h.uid.String() + "/" + id.String(),
		UploadedAt:   startedAt,
	})
	return id
}

func TestExportJSON_IncludeClips_AddsTopLevel(t *testing.T) {
	h := newHarness(t)
	seed(h, 1)
	seedClip(h, "clip-1", time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json?include=clips"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Events []map[string]any `json:"events"`
		Clips  []map[string]any `json:"clips"`
		Count  map[string]int   `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if len(resp.Events) != 1 {
		t.Errorf("events = %d, want 1", len(resp.Events))
	}
	if len(resp.Clips) != 1 {
		t.Errorf("clips = %d, want 1", len(resp.Clips))
	}
	if resp.Count["clips"] != 1 {
		t.Errorf("count.clips = %d, want 1 (raw=%v)", resp.Count["clips"], resp.Count)
	}
	if resp.Count["events"] != 1 {
		t.Errorf("count.events = %d, want 1", resp.Count["events"])
	}
}

func TestExportJSON_IncludeSessionsAndClips(t *testing.T) {
	h := newHarness(t)
	seed(h, 2)
	seedSession(h, "sess-A", time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC))
	seedSession(h, "sess-B", time.Date(2026, 5, 5, 2, 0, 0, 0, time.UTC))
	seedClip(h, "clip-X", time.Date(2026, 5, 5, 3, 0, 0, 0, time.UTC))

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json?include=sessions,clips"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Events   []map[string]any `json:"events"`
		Sessions []map[string]any `json:"sessions"`
		Clips    []map[string]any `json:"clips"`
		Count    map[string]int   `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	if len(resp.Sessions) != 2 || len(resp.Clips) != 1 || len(resp.Events) != 2 {
		t.Errorf("counts: sessions=%d, clips=%d, events=%d (want 2/1/2)",
			len(resp.Sessions), len(resp.Clips), len(resp.Events))
	}
	if resp.Count["sessions"] != 2 || resp.Count["clips"] != 1 || resp.Count["events"] != 2 {
		t.Errorf("count map = %v", resp.Count)
	}
}

func TestExportJSON_NoInclude_PreservesOriginalShape(t *testing.T) {
	h := newHarness(t)
	seed(h, 1)
	seedClip(h, "clip-X", time.Date(2026, 5, 5, 3, 0, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Original shape: count is a flat int, no clips/sessions keys.
	var resp struct {
		Events []map[string]any `json:"events"`
		Count  int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if resp.Count != 1 {
		t.Errorf("count = %d, want 1", resp.Count)
	}
	// Confirm we did NOT leak the clips key when include is absent.
	body := rec.Body.String()
	if strings.Contains(body, `"clips":`) {
		t.Errorf("backward-compat broken: clips key present without include=clips: %s", body)
	}
}

func TestExportJSON_RejectsBadInclude(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.json?include=sessions,bogus"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestExportCSV_IncludeClips_AddsSection(t *testing.T) {
	h := newHarness(t)
	seed(h, 1)
	seedClip(h, "clip-1", time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv?include=clips"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# section: events") {
		t.Errorf("events section header missing: %s", body)
	}
	if !strings.Contains(body, "# section: clips") {
		t.Errorf("clips section header missing: %s", body)
	}
	if !strings.Contains(body, "clip-1") {
		t.Errorf("clip-1 row missing: %s", body)
	}
}

func TestExportCSV_NoInclude_KeepsOriginalShape(t *testing.T) {
	h := newHarness(t)
	seed(h, 1)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, "/export/events.csv"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "# section:") {
		t.Errorf("section header leaked into vanilla CSV: %s", body)
	}
}
