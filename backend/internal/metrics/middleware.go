package metrics

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// HandlerDurationMiddleware records each request's wall-clock duration
// against http_handler_duration_seconds, labeled by the chi route
// pattern (e.g. "/events" rather than the raw URL). Falls back to the
// raw URL.Path when the chi route context is missing — that path is
// hit only for 404s, where bounded cardinality is preserved by the
// upstream "no route matched" handler returning a fixed pattern.
//
// Pass a nil receiver and the middleware short-circuits to a no-op
// pass-through.
func (i *Instruments) HandlerDurationMiddleware(next http.Handler) http.Handler {
	if i == nil || i.HandlerDuration == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		route := routeOf(r)
		i.RecordHandlerDuration(r.Context(), time.Since(start).Seconds(), route)
	})
}

// routeOf prefers the chi route pattern over r.URL.Path so the metric
// stays low-cardinality. Returns "/_unrouted" when neither is
// available — that path covers the bare 404 case.
func routeOf(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	if r.URL != nil && r.URL.Path != "" {
		return r.URL.Path
	}
	return "/_unrouted"
}
