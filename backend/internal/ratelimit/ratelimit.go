// Package ratelimit provides chi-friendly rate-limiting middleware
// keyed either by client IP (for unauthenticated routes) or by the
// authenticated user ID stashed on the request context by
// auth.Middleware (for everything past the bearer-token check).
//
// In-memory only — fine for the single-replica MVP. Distributed
// rate limiting (Redis/dragonfly backed) is explicit future work.
package ratelimit

import (
	"net/http"
	"time"

	"github.com/go-chi/httprate"

	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
)

// PerUser returns a chi middleware that limits to `requests` per
// `window` keyed by the authenticated user ID. Must run AFTER
// auth.Middleware so the user ID is present on the request context.
//
// Falls back to per-IP keying with a "ip-fallback:" prefix when the
// user ID is missing — a defense in depth so a misconfiguration that
// drops auth.Middleware doesn't accidentally remove the limit too.
func PerUser(requests int, window time.Duration) func(http.Handler) http.Handler {
	return httprate.Limit(
		requests, window,
		httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
			if uid, ok := auth.UserIDFrom(r.Context()); ok {
				return "user:" + uid.String(), nil
			}
			ip, err := httprate.KeyByIP(r)
			if err != nil {
				return "", err
			}
			return "ip-fallback:" + ip, nil
		}),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			httpkit.Error(w, http.StatusTooManyRequests, "rate limit exceeded; retry later")
		}),
	)
}

// PerIP returns a chi middleware that limits per client IP. Use for
// unauthenticated routes (auth/apple, auth/refresh, auth/logout)
// where the user ID isn't yet known.
func PerIP(requests int, window time.Duration) func(http.Handler) http.Handler {
	return httprate.Limit(
		requests, window,
		httprate.WithKeyFuncs(httprate.KeyByIP),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			httpkit.Error(w, http.StatusTooManyRequests, "rate limit exceeded; retry later")
		}),
	)
}
