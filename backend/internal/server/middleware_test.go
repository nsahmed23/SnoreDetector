package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
)

const middlewareTestKey = "test-signing-key-must-be-at-least-32-bytes-long!"

func newTestIssuer(t *testing.T) *authjwt.Issuer {
	t.Helper()
	iss, err := authjwt.New([]byte(middlewareTestKey), "iss", time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return iss
}

func TestAuthMiddleware_AcceptsValidToken(t *testing.T) {
	iss := newTestIssuer(t)
	uid := uuid.New()
	tok, _ := iss.Issue(uid)

	var seenUser uuid.UUID
	handler := AuthMiddleware(iss)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		t.Errorf("user not propagated to context: got %s, want %s", seenUser, uid)
	}
}

func TestAuthMiddleware_RejectsMissingHeader(t *testing.T) {
	iss := newTestIssuer(t)
	handler := AuthMiddleware(iss)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not run")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddleware_RejectsBadScheme(t *testing.T) {
	iss := newTestIssuer(t)
	handler := AuthMiddleware(iss)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestAuthMiddleware_RejectsBadToken(t *testing.T) {
	iss := newTestIssuer(t)
	handler := AuthMiddleware(iss)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestUserIDFrom_NotSet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := UserIDFrom(req.Context()); ok {
		t.Error("UserIDFrom should return ok=false when not set")
	}
}
