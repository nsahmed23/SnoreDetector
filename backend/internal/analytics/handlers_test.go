package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const testKey = "test-signing-key-must-be-at-least-32-bytes-long!"

type fakeStore struct {
	dailyRows []store.DailySummary
	totals    *store.Totals
	pingErr   error
	dailyErr  error
	totalsErr error
}

func (f *fakeStore) DailySummaries(_ context.Context, _ uuid.UUID, _ string, _, _ time.Time) ([]store.DailySummary, error) {
	if f.dailyErr != nil {
		return nil, f.dailyErr
	}
	return f.dailyRows, nil
}
func (f *fakeStore) TotalsSince(_ context.Context, _ uuid.UUID, _ time.Time) (*store.Totals, error) {
	if f.totalsErr != nil {
		return nil, f.totalsErr
	}
	if f.totals == nil {
		return &store.Totals{}, nil
	}
	return f.totals, nil
}
func (f *fakeStore) Ping(_ context.Context) error { return f.pingErr }
func (f *fakeStore) IsJTIRevoked(_ context.Context, _ string) (bool, error) {
	return false, nil
}

type harness struct {
	router http.Handler
	store  *fakeStore
	jwt    *authjwt.Issuer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	iss, err := authjwt.New([]byte(testKey), "test-iss", time.Hour)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	st := &fakeStore{}
	return &harness{
		router: New(Deps{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Store:  st,
			JWT:    iss,
		}),
		store: st,
		jwt:   iss,
	}
}

func (h *harness) authReq(t *testing.T, method, target string) *http.Request {
	t.Helper()
	tok, err := h.jwt.Issue(uuid.New())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	return r
}

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHealthz_Degraded(t *testing.T) {
	h := newHarness(t)
	h.store.pingErr = errors.New("pg gone")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestSummary_RequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analytics/summary", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSummary_RequiresStartEnd(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet, "/analytics/summary"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSummary_RejectsBadTZ(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet,
		"/analytics/summary?start=2026-05-01T00:00:00Z&end=2026-05-08T00:00:00Z&tz=Mars/Olympus"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSummary_RejectsReversedRange(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet,
		"/analytics/summary?start=2026-05-08T00:00:00Z&end=2026-05-01T00:00:00Z"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSummary_RejectsHugeRange(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet,
		"/analytics/summary?start=2020-01-01T00:00:00Z&end=2026-05-01T00:00:00Z"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSummary_FormatsRows(t *testing.T) {
	h := newHarness(t)
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	h.store.dailyRows = []store.DailySummary{
		{Day: day, EventCount: 5, TotalDurationMS: 12000, AvgDB: 65.5, LongestMS: 4000},
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet,
		"/analytics/summary?start=2026-05-01T00:00:00Z&end=2026-05-08T00:00:00Z"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp dailySummaryResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Days) != 1 || resp.Days[0].Day != "2026-05-04" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.Days[0].EventCount != 5 {
		t.Errorf("event_count = %d", resp.Days[0].EventCount)
	}
}

func TestTotals_Empty(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet, "/analytics/totals"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestTotals_RejectsBadSince(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet, "/analytics/totals?since=yesterday"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestTotals_PassesThrough(t *testing.T) {
	h := newHarness(t)
	first := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	last := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	h.store.totals = &store.Totals{
		EventCount: 42, TotalDurationMS: 999, AvgDB: 71.2, LongestMS: 5000,
		FirstEventAt: &first, LastEventAt: &last,
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, h.authReq(t, http.MethodGet, "/analytics/totals"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp totalsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.EventCount != 42 || resp.LongestMS != 5000 {
		t.Errorf("unexpected: %+v", resp)
	}
	if resp.FirstEventAt == nil || resp.LastEventAt == nil {
		t.Errorf("first/last not propagated: %+v", resp)
	}
}
