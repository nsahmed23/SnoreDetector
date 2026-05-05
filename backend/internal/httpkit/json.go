// Package httpkit holds tiny HTTP helpers shared by every service's
// handlers. It exists so adding a new service doesn't drag in the
// whole sync-service `server` package just for `WriteJSON`.
package httpkit

import (
	"encoding/json"
	"net/http"
)

// JSON writes `body` as JSON with the given status. Errors during
// encoding are logged via the response (best-effort) but not
// propagated — once headers are written there's nothing useful to do.
func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Error writes a `{"error": "<msg>"}` response with the given status.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}
