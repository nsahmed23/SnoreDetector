package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// signIn drives /auth/apple end-to-end and returns the token pair.
func signIn(t *testing.T, h *harness) tokenResponse {
	t.Helper()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	tok := h.appleToken(t, validAppleClaims(now))
	body, _ := json.Marshal(map[string]string{"identity_token": tok})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/apple", bytes.NewReader(body))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apple sign-in: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func postJSON(t *testing.T, h *harness, path, bearer string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// --------- /auth/apple shape ---------

func TestAuthApple_IncludesRefreshToken(t *testing.T) {
	h := newHarness(t)
	tok := signIn(t, h)
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", tok)
	}
	if tok.AccessToken == tok.RefreshToken {
		t.Error("access and refresh tokens should differ")
	}
	if !tok.AccessTokenExpiresAt.Before(tok.RefreshTokenExpiresAt) {
		t.Errorf("access expiry %v should be earlier than refresh %v",
			tok.AccessTokenExpiresAt, tok.RefreshTokenExpiresAt)
	}
}

// --------- /auth/refresh ---------

func TestRefresh_HappyPath(t *testing.T) {
	h := newHarness(t)
	first := signIn(t, h)

	rec := postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var second tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.AccessToken == first.AccessToken {
		t.Error("access token should be rotated")
	}
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh token should be rotated")
	}
	if second.UserID != first.UserID {
		t.Errorf("user id changed: %s -> %s", first.UserID, second.UserID)
	}

	// The original refresh token must be rejected on a second call.
	rec = postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reusing old refresh: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRefresh_RejectsBadJSON(t *testing.T) {
	h := newHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader("nope"))
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestRefresh_RejectsMissingToken(t *testing.T) {
	h := newHarness(t)
	rec := postJSON(t, h, "/auth/refresh", "", map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestRefresh_RejectsAccessTokenAsRefresh(t *testing.T) {
	// Access tokens carry a different `iss` claim, so they must fail
	// VerifyRefresh — this guards against clients (or attackers)
	// passing the access token to /auth/refresh.
	h := newHarness(t)
	pair := signIn(t, h)
	rec := postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": pair.AccessToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", rec.Code)
	}
}

func TestRefresh_ExpiredFails(t *testing.T) {
	h := newHarness(t)
	pair := signIn(t, h)

	// Forge a refresh JWT with an `exp` in the past. We sign it with
	// the harness's signing key + refresh issuer so it would otherwise
	// validate.
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	claims := jwt.MapClaims{
		"iss": "test-refresh-iss",
		"sub": pair.UserID,
		"uid": pair.UserID,
		"fid": uuid.New().String(),
		"jti": uuid.New().String(),
		"iat": past.Unix(),
		"nbf": past.Unix(),
		"exp": past.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte("test-signing-key-must-be-at-least-32-bytes-long!"))
	if err != nil {
		t.Fatal(err)
	}

	rec := postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": signed,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
}

func TestRefresh_RevokedFails(t *testing.T) {
	h := newHarness(t)
	pair := signIn(t, h)

	// Mark every issued refresh row as revoked.
	for _, rt := range h.store.refreshTokens {
		now := time.Now().UTC()
		rt.RevokedAt = &now
	}

	rec := postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": pair.RefreshToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}
}

func TestRefresh_ReuseRevokesFamily(t *testing.T) {
	h := newHarness(t)
	first := signIn(t, h)

	// Rotate once normally.
	rec := postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("first rotate: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var second tokenResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &second)

	// Now replay the original refresh token. Theft response: 401.
	rec = postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reuse: status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}

	// And the legitimate (rotated) token must now also fail —
	// the entire family should have been revoked.
	rec = postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": second.RefreshToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("rotated token after theft: status=%d body=%s, want 401",
			rec.Code, rec.Body.String())
	}
}

// --------- /auth/logout + JTI revocation ---------

func TestLogout_RevokesAccessToken(t *testing.T) {
	h := newHarness(t)
	pair := signIn(t, h)

	// Access token must work before logout.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-logout GET /events: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Log out.
	rec = postJSON(t, h, "/auth/logout", pair.AccessToken, map[string]string{
		"refresh_token": pair.RefreshToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Same access token must now be rejected.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	h.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("post-logout: status=%d body=%s, want 401", rec.Code, rec.Body.String())
	}

	// And the refresh token must be rejected too (family revoked).
	rec = postJSON(t, h, "/auth/refresh", "", map[string]string{
		"refresh_token": pair.RefreshToken,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("post-logout refresh: status=%d, want 401", rec.Code)
	}
}

func TestLogout_RequiresAuth(t *testing.T) {
	h := newHarness(t)
	rec := postJSON(t, h, "/auth/logout", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", rec.Code)
	}
}
