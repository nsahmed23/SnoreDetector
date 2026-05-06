package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/apple"
	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

// fakeStore is an in-memory Store for handler tests.
type fakeStore struct {
	mu             sync.Mutex
	users          map[string]*store.User // keyed by apple_subject
	events         []store.SnoreEvent
	refreshTokens  map[uuid.UUID]*store.RefreshToken
	revokedJTIs    map[string]uuid.UUID
	upsertErr      error
	insertErr      error
	listErr        error
	pingErr        error
	upsertCalls    int
	now            func() time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:         map[string]*store.User{},
		refreshTokens: map[uuid.UUID]*store.RefreshToken{},
		revokedJTIs:   map[string]uuid.UUID{},
		now:           func() time.Time { return time.Now().UTC() },
	}
}

func (f *fakeStore) UpsertUser(_ context.Context, sub, email string, isPriv bool) (*store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertCalls++
	if f.upsertErr != nil {
		return nil, f.upsertErr
	}
	if u, ok := f.users[sub]; ok {
		u.LastSeenAt = time.Now().UTC()
		if email != "" {
			u.Email = email
		}
		u.IsPrivateEmail = isPriv
		return u, nil
	}
	u := &store.User{
		ID:             uuid.New(),
		AppleSubject:   sub,
		Email:          email,
		IsPrivateEmail: isPriv,
		CreatedAt:      time.Now().UTC(),
		LastSeenAt:     time.Now().UTC(),
	}
	f.users[sub] = u
	return u, nil
}

func (f *fakeStore) InsertEvents(_ context.Context, uid uuid.UUID, evs []store.SnoreEvent) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	seen := map[string]bool{}
	for _, e := range f.events {
		if e.UserID == uid {
			seen[e.ClientEventID] = true
		}
	}
	inserted := 0
	for _, e := range evs {
		if seen[e.ClientEventID] {
			continue
		}
		e.ID = uuid.New()
		e.UserID = uid
		e.ReceivedAt = time.Now().UTC()
		f.events = append(f.events, e)
		seen[e.ClientEventID] = true
		inserted++
	}
	return inserted, nil
}

func (f *fakeStore) ListEvents(_ context.Context, uid uuid.UUID, cursor store.Cursor, limit int) ([]store.SnoreEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	// Sort by (received_at, id) ascending so the fake matches the
	// real store's compound-cursor ordering.
	mine := []store.SnoreEvent{}
	for _, e := range f.events {
		if e.UserID != uid {
			continue
		}
		mine = append(mine, e)
	}
	sort.Slice(mine, func(i, j int) bool {
		if mine[i].ReceivedAt.Equal(mine[j].ReceivedAt) {
			return mine[i].ID.String() < mine[j].ID.String()
		}
		return mine[i].ReceivedAt.Before(mine[j].ReceivedAt)
	})
	out := []store.SnoreEvent{}
	for _, e := range mine {
		if rowGreater(e.ReceivedAt, e.ID, cursor.ReceivedAt, cursor.ID) {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// rowGreater compares (a_ts, a_id) > (b_ts, b_id) lexicographically.
func rowGreater(at time.Time, aid uuid.UUID, bt time.Time, bid uuid.UUID) bool {
	if at.After(bt) {
		return true
	}
	if at.Before(bt) {
		return false
	}
	return aid.String() > bid.String()
}

func (f *fakeStore) Ping(_ context.Context) error { return f.pingErr }

func (f *fakeStore) RecordRefreshToken(_ context.Context, tokenID, userID, familyID uuid.UUID, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshTokens[tokenID] = &store.RefreshToken{
		TokenID:   tokenID,
		UserID:    userID,
		FamilyID:  familyID,
		IssuedAt:  f.now(),
		ExpiresAt: expiresAt,
	}
	return nil
}

func (f *fakeStore) GetRefreshToken(_ context.Context, tokenID uuid.UUID) (*store.RefreshToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt, ok := f.refreshTokens[tokenID]
	if !ok {
		return nil, store.ErrRefreshNotFound
	}
	// Return a copy so callers can't mutate the fake's state.
	cp := *rt
	if rt.RevokedAt != nil {
		t := *rt.RevokedAt
		cp.RevokedAt = &t
	}
	if rt.ReplacedBy != nil {
		id := *rt.ReplacedBy
		cp.ReplacedBy = &id
	}
	return &cp, nil
}

func (f *fakeStore) ReplaceRefreshToken(_ context.Context, oldID, newID, userID, familyID uuid.UUID, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	old, ok := f.refreshTokens[oldID]
	if !ok {
		return store.ErrRefreshNotFound
	}
	if old.ReplacedBy != nil || old.RevokedAt != nil {
		return store.ErrAlreadyRotated
	}
	f.refreshTokens[newID] = &store.RefreshToken{
		TokenID:   newID,
		UserID:    userID,
		FamilyID:  familyID,
		IssuedAt:  f.now(),
		ExpiresAt: expiresAt,
	}
	id := newID
	old.ReplacedBy = &id
	return nil
}

func (f *fakeStore) RevokeRefreshFamily(_ context.Context, userID, familyID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for _, rt := range f.refreshTokens {
		if rt.UserID == userID && rt.FamilyID == familyID && rt.RevokedAt == nil {
			t := now
			rt.RevokedAt = &t
		}
	}
	return nil
}

func (f *fakeStore) RecordRevokedJTI(_ context.Context, jti string, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokedJTIs[jti] = userID
	return nil
}

func (f *fakeStore) IsJTIRevoked(_ context.Context, jti string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.revokedJTIs[jti]
	return ok, nil
}

// helpers for building a fully-wired server
type harness struct {
	server *http.Server
	router http.Handler
	store  *fakeStore
	jwt    *authjwt.Issuer
	apple  *apple.Verifier
	priv   *rsa.PrivateKey
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	v := apple.NewWithKeyfunc(
		"com.snoreguard.app",
		"https://appleid.apple.com",
		func(_ *jwt.Token) (any, error) { return &priv.PublicKey, nil },
	)
	v.SetClock(func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) })
	iss, err := authjwt.NewWithRefresh(
		[]byte("test-signing-key-must-be-at-least-32-bytes-long!"),
		"test-iss", "test-refresh-iss",
		time.Hour, 24*time.Hour,
	)
	if err != nil {
		t.Fatalf("jwt.New: %v", err)
	}
	st := newFakeStore()
	deps := Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:  st,
		Apple:  v,
		JWT:    iss,
		Now:    func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) },
	}
	return &harness{
		router: New(deps),
		store:  st,
		jwt:    iss,
		apple:  v,
		priv:   priv,
	}
}

func (h *harness) appleToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "test-kid"
	s, err := tok.SignedString(h.priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func (h *harness) sessionToken(t *testing.T, uid uuid.UUID) string {
	t.Helper()
	tok, err := h.jwt.Issue(uid)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return tok
}

func validAppleClaims(now time.Time) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":              "https://appleid.apple.com",
		"aud":              "com.snoreguard.app",
		"sub":              "001234.fakeappleuser.0001",
		"iat":              now.Add(-1 * time.Minute).Unix(),
		"exp":              now.Add(10 * time.Minute).Unix(),
		"email":            "u@privaterelay.appleid.com",
		"email_verified":   "true",
		"is_private_email": "true",
	}
}

// --------- /healthz ---------

func TestHealthz_OK(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestHealthz_Degraded(t *testing.T) {
	h := newHarness(t)
	h.store.pingErr = errors.New("pg gone")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// --------- /auth/apple ---------

func TestAuthApple_Success(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	tok := h.appleToken(t, validAppleClaims(now))
	body, _ := json.Marshal(map[string]string{"identity_token": tok})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.UserID == "" {
		t.Error("user_id missing")
	}
	if resp.AccessToken == "" {
		t.Error("access_token missing")
	}
	if h.store.upsertCalls != 1 {
		t.Errorf("upsertCalls = %d", h.store.upsertCalls)
	}
}

func TestAuthApple_Idempotent(t *testing.T) {
	// Two sign-ins with the same Apple subject must reuse the same user row.
	h := newHarness(t)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	tok := h.appleToken(t, validAppleClaims(now))
	body, _ := json.Marshal(map[string]string{"identity_token": tok})

	doRequest := func() string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
		h.router.ServeHTTP(rec, req)
		var resp struct {
			UserID string `json:"user_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.UserID
	}
	first := doRequest()
	second := doRequest()
	if first == "" || first != second {
		t.Errorf("user_id should match: %q vs %q", first, second)
	}
}

func TestAuthApple_RejectsBadJSON(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", strings.NewReader("not-json"))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAuthApple_RejectsMissingToken(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", strings.NewReader(`{"identity_token":""}`))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAuthApple_RejectsExpiredAppleToken(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	c := validAppleClaims(now)
	c["exp"] = now.Add(-1 * time.Minute).Unix()
	tok := h.appleToken(t, c)
	body, _ := json.Marshal(map[string]string{"identity_token": tok})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// --------- /events POST ---------

func TestPostEvents_Success(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	body := map[string]any{
		"events": []map[string]any{
			{
				"client_event_id": "ev-1",
				"started_at":      "2026-05-05T03:14:00Z",
				"duration_ms":     1500,
				"avg_db":          62.5,
			},
		},
	}
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp createEventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Inserted != 1 || resp.Received != 1 {
		t.Errorf("response = %+v", resp)
	}
}

func TestPostEvents_DedupesByClientEventID(t *testing.T) {
	h := newHarness(t)
	uid := uuid.New()
	tok := h.sessionToken(t, uid)
	body := map[string]any{
		"events": []map[string]any{
			{"client_event_id": "ev-1", "started_at": "2026-05-05T03:14:00Z", "duration_ms": 1000, "avg_db": 60},
			{"client_event_id": "ev-1", "started_at": "2026-05-05T03:14:00Z", "duration_ms": 1000, "avg_db": 60},
		},
	}
	b, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp createEventsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Inserted != 1 || resp.Received != 2 {
		t.Errorf("dedupe broken: %+v", resp)
	}
}

func TestPostEvents_RequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(`{"events":[]}`))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestPostEvents_RejectsEmptyArray(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(`{"events":[]}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPostEvents_RejectsTooMany(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	events := make([]map[string]any, maxEventsPerRequest+1)
	for i := range events {
		events[i] = map[string]any{
			"client_event_id": "ev-" + uuid.NewString(),
			"started_at":      "2026-05-05T03:14:00Z",
			"duration_ms":     1000, "avg_db": 60.0,
		}
	}
	b, _ := json.Marshal(map[string]any{"events": events})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPostEvents_RejectsInvalidEvent(t *testing.T) {
	cases := []struct {
		name  string
		event map[string]any
	}{
		{"missing client_event_id", map[string]any{
			"started_at": "2026-05-05T03:14:00Z", "duration_ms": 1000, "avg_db": 60.0,
		}},
		{"negative duration", map[string]any{
			"client_event_id": "ev-1",
			"started_at":      "2026-05-05T03:14:00Z", "duration_ms": -1, "avg_db": 60.0,
		}},
		{"missing started_at", map[string]any{
			"client_event_id": "ev-1", "duration_ms": 1000, "avg_db": 60.0,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tok := h.sessionToken(t, uuid.New())
			b, _ := json.Marshal(map[string]any{"events": []any{tc.event}})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(b))
			req.Header.Set("Authorization", "Bearer "+tok)
			h.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// --------- /events GET ---------

func TestGetEvents_ReturnsOnlyOwnEvents(t *testing.T) {
	h := newHarness(t)
	mine := uuid.New()
	other := uuid.New()
	h.store.events = []store.SnoreEvent{
		{UserID: mine, ClientEventID: "a", StartedAt: time.Now(), DurationMS: 1, AvgDB: 60, ReceivedAt: time.Now()},
		{UserID: other, ClientEventID: "b", StartedAt: time.Now(), DurationMS: 1, AvgDB: 60, ReceivedAt: time.Now()},
	}
	tok := h.sessionToken(t, mine)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp listEventsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Events) != 1 || resp.Events[0].ClientEventID != "a" {
		t.Errorf("returned wrong events: %+v", resp.Events)
	}
}

func TestGetEvents_RejectsBadSince(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?since=not-a-date", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestGetEvents_RejectsBadLimit(t *testing.T) {
	h := newHarness(t)
	tok := h.sessionToken(t, uuid.New())
	for _, l := range []string{"-5", "0", "9999", "abc"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/events?limit="+l, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		h.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want 400", l, rec.Code)
		}
	}
}
