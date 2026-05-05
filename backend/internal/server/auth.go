package server

import (
	"encoding/json"
	"net/http"
	"time"
)

type authHandler struct {
	deps Deps
}

type appleSignInRequest struct {
	IdentityToken string `json:"identity_token"`
}

type appleSignInResponse struct {
	UserID      string    `json:"user_id"`
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// handle accepts an Apple identity token, verifies it, upserts the
// user, and returns a session JWT.
func (h *authHandler) handle(w http.ResponseWriter, r *http.Request) {
	var req appleSignInRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.IdentityToken == "" {
		writeError(w, http.StatusBadRequest, "identity_token is required")
		return
	}
	if h.deps.Apple == nil || h.deps.JWT == nil || h.deps.Store == nil {
		writeError(w, http.StatusInternalServerError, "auth not configured")
		return
	}

	id, err := h.deps.Apple.Verify(req.IdentityToken)
	if err != nil {
		h.deps.Logger.Warn("apple verify failed", "err", err.Error())
		writeError(w, http.StatusUnauthorized, "identity token rejected")
		return
	}

	user, err := h.deps.Store.UpsertUser(r.Context(), id.Subject, id.Email, id.IsPrivateEmail)
	if err != nil {
		h.deps.Logger.Error("upsert user failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to record user")
		return
	}

	tok, err := h.deps.JWT.Issue(user.ID)
	if err != nil {
		// jwt.Issue is currently infallible for valid configs, but
		// surface a clear error if signing ever breaks.
		h.deps.Logger.Error("jwt issue failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to issue session")
		return
	}

	// We re-derive expiry by parsing the token rather than threading
	// it through Issue so the issuer remains the single source of truth.
	expiresAt := h.deps.Now().Add(jwtTTL(h.deps))

	writeJSON(w, http.StatusOK, appleSignInResponse{
		UserID:      user.ID.String(),
		AccessToken: tok,
		ExpiresAt:   expiresAt,
	})
}

func jwtTTL(d Deps) time.Duration {
	if d.JWT == nil {
		return 30 * 24 * time.Hour
	}
	return d.JWT.TTL()
}
