package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
	"github.com/nsahmed23/SnoreDetector/backend/internal/metrics"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

type authHandler struct {
	deps Deps
}

type appleSignInRequest struct {
	IdentityToken string `json:"identity_token"`
}

// tokenResponse is the shape returned by both /auth/apple and
// /auth/refresh — a fresh access + refresh pair plus their expiries.
type tokenResponse struct {
	UserID                string    `json:"user_id"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

// handle accepts an Apple identity token, verifies it, upserts the
// user, and returns a session JWT + refresh token.
func (h *authHandler) handle(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "sync.auth.apple",
		trace.WithAttributes(
			attribute.String("endpoint", "sync.auth.apple"),
			semconv.HTTPRoute("/auth/apple"),
		),
	)
	defer span.End()

	var req appleSignInRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeBadRequest)
		httpkit.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.IdentityToken == "" {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeBadRequest)
		httpkit.Error(w, http.StatusBadRequest, "identity_token is required")
		return
	}
	if h.deps.Apple == nil || h.deps.JWT == nil || h.deps.Store == nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeMisconfigured)
		httpkit.Error(w, http.StatusInternalServerError, "auth not configured")
		return
	}

	id, err := h.deps.Apple.Verify(req.IdentityToken)
	if err != nil {
		h.deps.Logger.Warn("apple verify failed", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeInvalidToken)
		httpkit.Error(w, http.StatusUnauthorized, "identity token rejected")
		return
	}

	user, err := h.deps.Store.UpsertUser(ctx, id.Subject, id.Email, id.IsPrivateEmail)
	if err != nil {
		h.deps.Logger.Error("upsert user failed", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeStoreError)
		httpkit.Error(w, http.StatusInternalServerError, "failed to record user")
		return
	}
	span.SetAttributes(attribute.String("user_id_hash", metrics.HashUserID(user.ID)))

	// Brand-new sign-in => fresh refresh-token family.
	familyID := uuid.New()
	resp, err := h.issueTokenPair(r, user.ID, familyID)
	if err != nil {
		h.deps.Logger.Error("issue tokens", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeIssueFailed)
		httpkit.Error(w, http.StatusInternalServerError, "failed to issue session")
		return
	}
	span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusOK))
	h.deps.Metrics.AddAuthAttempt(ctx, metrics.OutcomeSuccess)
	httpkit.JSON(w, http.StatusOK, resp)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// refresh exchanges a valid refresh token for a new access + refresh
// pair, rotating the refresh token on use. If the presented refresh
// token has already been rotated, the entire family is revoked
// (theft response).
func (h *authHandler) refresh(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "sync.auth.refresh",
		trace.WithAttributes(
			attribute.String("endpoint", "sync.auth.refresh"),
			semconv.HTTPRoute("/auth/refresh"),
		),
	)
	defer span.End()

	var req refreshRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req); err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeBadRequest)
		httpkit.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RefreshToken == "" {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusBadRequest))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeBadRequest)
		httpkit.Error(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	if h.deps.JWT == nil || h.deps.Store == nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeMisconfigured)
		httpkit.Error(w, http.StatusInternalServerError, "auth not configured")
		return
	}

	claims, err := h.deps.JWT.VerifyRefresh(req.RefreshToken)
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeInvalidToken)
		httpkit.Error(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeInvalidToken)
		httpkit.Error(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeInvalidToken)
		httpkit.Error(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	tokenID, err := uuid.Parse(claims.ID)
	if err != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeInvalidToken)
		httpkit.Error(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	span.SetAttributes(attribute.String("user_id_hash", metrics.HashUserID(userID)))

	row, err := h.deps.Store.GetRefreshToken(ctx, tokenID)
	if err != nil {
		if errors.Is(err, store.ErrRefreshNotFound) {
			span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
			h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeNotFound)
			httpkit.Error(w, http.StatusUnauthorized, "refresh token unknown")
			return
		}
		h.deps.Logger.Error("get refresh token", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeStoreError)
		httpkit.Error(w, http.StatusInternalServerError, "auth check failed")
		return
	}
	if row.RevokedAt != nil {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeRevoked)
		httpkit.Error(w, http.StatusUnauthorized, "refresh token revoked")
		return
	}
	if row.ReplacedBy != nil {
		// Theft response: somebody is replaying an already-rotated
		// token. Revoke the entire family so the legitimate holder is
		// also forced to re-auth.
		if rerr := h.deps.Store.RevokeRefreshFamily(ctx, userID, familyID); rerr != nil {
			h.deps.Logger.Error("revoke family", "err", rerr.Error())
		} else {
			h.deps.Logger.Warn("refresh-token reuse detected; family revoked",
				"user", userID.String(), "family", familyID.String())
		}
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeTheftDetected)
		httpkit.Error(w, http.StatusUnauthorized, "refresh token reuse detected")
		return
	}

	// Mint the next pair, then atomically link old -> new.
	newAccess, _, accessExp, err := h.deps.JWT.IssueAccess(userID)
	if err != nil {
		h.deps.Logger.Error("issue access", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeIssueFailed)
		httpkit.Error(w, http.StatusInternalServerError, "failed to issue session")
		return
	}
	newRefresh, newRefreshID, refreshExp, err := h.deps.JWT.IssueRefresh(userID, familyID)
	if err != nil {
		h.deps.Logger.Error("issue refresh", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeIssueFailed)
		httpkit.Error(w, http.StatusInternalServerError, "failed to issue session")
		return
	}
	if err := h.deps.Store.ReplaceRefreshToken(ctx, tokenID, newRefreshID, userID, familyID, refreshExp); err != nil {
		if errors.Is(err, store.ErrAlreadyRotated) {
			// A racing /auth/refresh won — treat as theft response.
			_ = h.deps.Store.RevokeRefreshFamily(ctx, userID, familyID)
			span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
			h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeTheftDetected)
			httpkit.Error(w, http.StatusUnauthorized, "refresh token reuse detected")
			return
		}
		h.deps.Logger.Error("replace refresh token", "err", err.Error())
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
		h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeStoreError)
		httpkit.Error(w, http.StatusInternalServerError, "failed to rotate session")
		return
	}

	span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusOK))
	h.deps.Metrics.AddRefreshAttempt(ctx, metrics.OutcomeSuccess)
	httpkit.JSON(w, http.StatusOK, tokenResponse{
		UserID:                userID.String(),
		AccessToken:           newAccess,
		AccessTokenExpiresAt:  accessExp,
		RefreshToken:          newRefresh,
		RefreshTokenExpiresAt: refreshExp,
	})
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// logout revokes the access-token's JTI and (if a refresh token is
// supplied) the entire refresh-token family. Requires a valid access
// token in the Authorization header.
func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer(tracerName).Start(r.Context(), "sync.auth.logout",
		trace.WithAttributes(
			attribute.String("endpoint", "sync.auth.logout"),
			semconv.HTTPRoute("/auth/logout"),
		),
	)
	defer span.End()

	uid, ok := auth.UserIDFrom(ctx)
	if !ok {
		span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusUnauthorized))
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	span.SetAttributes(attribute.String("user_id_hash", metrics.HashUserID(uid)))
	jti, _ := auth.JTIFrom(ctx)

	var req logoutRequest
	// Body is optional — clients without a refresh token can still
	// log out the current access token.
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req)
	}

	if jti != "" {
		if err := h.deps.Store.RecordRevokedJTI(ctx, jti, uid); err != nil {
			h.deps.Logger.Error("revoke jti", "err", err.Error())
			span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
			httpkit.Error(w, http.StatusInternalServerError, "logout failed")
			return
		}
	}

	if req.RefreshToken != "" {
		if claims, err := h.deps.JWT.VerifyRefresh(req.RefreshToken); err == nil {
			if fid, ferr := uuid.Parse(claims.FamilyID); ferr == nil {
				if u, uerr := uuid.Parse(claims.UserID); uerr == nil && u == uid {
					if rerr := h.deps.Store.RevokeRefreshFamily(ctx, uid, fid); rerr != nil {
						h.deps.Logger.Error("revoke family", "err", rerr.Error())
					}
				}
			}
		}
	}

	span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusOK))
	httpkit.JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// issueTokenPair mints a fresh access + refresh pair for the given
// user/family and persists the refresh-token row.
func (h *authHandler) issueTokenPair(r *http.Request, userID, familyID uuid.UUID) (tokenResponse, error) {
	access, _, accessExp, err := h.deps.JWT.IssueAccess(userID)
	if err != nil {
		return tokenResponse{}, err
	}
	refresh, refreshID, refreshExp, err := h.deps.JWT.IssueRefresh(userID, familyID)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := h.deps.Store.RecordRefreshToken(r.Context(), refreshID, userID, familyID, refreshExp); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{
		UserID:                userID.String(),
		AccessToken:           access,
		AccessTokenExpiresAt:  accessExp,
		RefreshToken:          refresh,
		RefreshTokenExpiresAt: refreshExp,
	}, nil
}
