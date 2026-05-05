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

const ctxKeyUserID ctxKey = iota

// AuthMiddleware enforces a valid Bearer token and stashes the user
// ID on the request context.
func AuthMiddleware(iss *authjwt.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok, err := bearerToken(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}
			uid, err := iss.Verify(tok)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid session token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUserID, uid)
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
