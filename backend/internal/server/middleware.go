package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	authjwt "github.com/nsahmed23/SnoreDetector/backend/internal/jwt"
)

type ctxKey int

const (
	ctxKeyUserID ctxKey = iota
	ctxKeyJTI
)

// jtiChecker is the subset of Store the auth middleware needs to
// short-circuit on revoked access tokens. We accept an interface
// (rather than the full Store) so tests that don't exercise revocation
// can pass nil.
type jtiChecker interface {
	IsJTIRevoked(ctx context.Context, jti string) (bool, error)
}

// AuthMiddleware enforces a valid Bearer token and stashes the user
// ID + JTI on the request context. If `revoked` is non-nil it is
// consulted on every request; a 401 is returned for revoked JTIs.
func AuthMiddleware(iss *authjwt.Issuer, revoked jtiChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, err := bearerToken(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}
			claims, err := iss.VerifyAccess(tok)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid session token")
				return
			}
			uid, err := uuid.Parse(claims.UserID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid session token")
				return
			}
			if revoked != nil && claims.ID != "" {
				gone, err := revoked.IsJTIRevoked(r.Context(), claims.ID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "auth check failed")
					return
				}
				if gone {
					writeError(w, http.StatusUnauthorized, "session revoked")
					return
				}
			}
			ctx := context.WithValue(r.Context(), ctxKeyUserID, uid)
			ctx = context.WithValue(ctx, ctxKeyJTI, claims.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFrom returns the authenticated user ID for a request, or
// uuid.Nil + false if the middleware didn't authenticate it.
func UserIDFrom(ctx context.Context) (uuid.UUID, bool) {
	u, ok := ctx.Value(ctxKeyUserID).(uuid.UUID)
	return u, ok
}

// JTIFrom returns the access-token JTI from the request context.
func JTIFrom(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(ctxKeyJTI).(string)
	return s, ok && s != ""
}

func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errors.New("missing Authorization")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", errors.New("not a Bearer token")
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", errors.New("empty token")
	}
	return tok, nil
}
