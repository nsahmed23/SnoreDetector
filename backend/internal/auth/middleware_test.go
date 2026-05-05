package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
)

const testKey = "test-signing-key-must-be-at-least-32-bytes-long!"

func newTestIssuer(t *testing.T) *authjwt.Issuer {
	t.Helper()
	iss, err := authjwt.New([]byte(testKey), "iss", time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return iss
}

// fakeJTIStore is a tiny test double for JTIStore.
type fakeJTIStore struct {
	revoked map[string]bool
	err     error
}

func (f *fakeJTIStore) IsJTIRevoked(_ context.Context, jti string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.revoked[jti], nil
}

func TestMiddleware_AcceptsValidToken(t *testing.T) {
	iss := newTestIssuer(t)
	uid := uuid.New()
	tok, _ := iss.Issue(uid)

	var seenUser uuid.UUID
	handler := Middleware(iss, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUser, _ = UserIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seenUser != uid {
		t.Errorf("user not propagated: got %s, want %s", seenUser, uid)
	}
}

func TestMiddleware_RejectsMissingHeader(t *testing.T) {
	iss := newTestIssuer(t)
	handler := Middleware(iss, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not run")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_RejectsBadScheme(t *testing.T) {
	iss := newTestIssuer(t)
	handler := Middleware(iss, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not run")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_RejectsBadToken(t *testing.T) {
	iss := newTestIssuer(t)
	handler := Middleware(iss, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not run")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_RejectsRevokedJTI(t *testing.T) {
	iss := newTestIssuer(t)
	uid := uuid.New()
	tok, jti, _, err := iss.IssueAccess(uid)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	store := &fakeJTIStore{revoked: map[string]bool{jti.String(): true}}
	handler := Middleware(iss, store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not run for revoked JTI")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_PropagatesJTIStoreError(t *testing.T) {
	iss := newTestIssuer(t)
	uid := uuid.New()
	tok, _, _, err := iss.IssueAccess(uid)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	store := &fakeJTIStore{err: errors.New("boom")}
	handler := Middleware(iss, store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not run when JTI check errs")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestUserIDFrom_NotSet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := UserIDFrom(req.Context()); ok {
		t.Error("UserIDFrom should return ok=false when not set")
	}
}
