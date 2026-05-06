package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

// Recording-session handlers. Sessions are the per-recording-run unit
// the iOS app attaches snore events + audio clips to.

const (
	maxSessionRequestBytes = 8 * 1024
	defaultSessionLimit    = 100
)

type sessionsHandler struct {
	deps Deps
}

type postSessionRequest struct {
	ClientSessionID string     `json:"client_session_id"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DeviceName      string     `json:"device_name,omitempty"`
	AppVersion      string     `json:"app_version,omitempty"`
}

type postSessionResponse struct {
	SessionID string `json:"session_id"`
	Created   bool   `json:"created"`
}

func (h *sessionsHandler) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req postSessionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSessionRequestBytes)).Decode(&req); err != nil {
		httpkit.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateSession(req); err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, created, err := h.deps.Store.UpsertSession(r.Context(), uid, store.RecordingSession{
		ClientSessionID: req.ClientSessionID,
		StartedAt:       req.StartedAt,
		EndedAt:         req.EndedAt,
		DeviceName:      req.DeviceName,
		AppVersion:      req.AppVersion,
	})
	if err != nil {
		h.deps.Logger.Error("upsert session", "err", err.Error(), "user", uid.String())
		httpkit.Error(w, http.StatusInternalServerError, "failed to record session")
		return
	}
	httpkit.JSON(w, http.StatusOK, postSessionResponse{
		SessionID: sess.ID.String(),
		Created:   created,
	})
}

type sessionDTO struct {
	SessionID       string     `json:"session_id"`
	ClientSessionID string     `json:"client_session_id"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DeviceName      string     `json:"device_name,omitempty"`
	AppVersion      string     `json:"app_version,omitempty"`
}

type listSessionsResponse struct {
	Sessions   []sessionDTO `json:"sessions"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (h *sessionsHandler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	q := r.URL.Query()
	cursor, err := decodeDescCursor(q.Get("cursor"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, "invalid 'cursor' parameter")
		return
	}
	limit := defaultSessionLimit
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 || n > 1000 {
			httpkit.Error(w, http.StatusBadRequest, "invalid 'limit' (1..1000)")
			return
		}
		limit = n
	}

	rows, err := h.deps.Store.ListSessions(r.Context(), uid, cursor, limit)
	if err != nil {
		h.deps.Logger.Error("list sessions", "err", err.Error())
		httpkit.Error(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	out := make([]sessionDTO, 0, len(rows))
	for _, s := range rows {
		out = append(out, sessionDTO{
			SessionID:       s.ID.String(),
			ClientSessionID: s.ClientSessionID,
			StartedAt:       s.StartedAt,
			EndedAt:         s.EndedAt,
			DeviceName:      s.DeviceName,
			AppVersion:      s.AppVersion,
		})
	}
	resp := listSessionsResponse{Sessions: out}
	if len(rows) == limit && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeDescCursor(store.DescCursor{StartedAt: last.StartedAt, ID: last.ID})
	}
	httpkit.JSON(w, http.StatusOK, resp)
}

func validateSession(req postSessionRequest) error {
	if req.ClientSessionID == "" {
		return errors.New("client_session_id is required")
	}
	if len(req.ClientSessionID) > 128 {
		return errors.New("client_session_id too long")
	}
	if req.StartedAt.IsZero() {
		return errors.New("started_at is required")
	}
	if req.EndedAt != nil && req.EndedAt.Before(req.StartedAt) {
		return errors.New("ended_at must be ≥ started_at")
	}
	if len(req.DeviceName) > 256 {
		return errors.New("device_name too long")
	}
	if len(req.AppVersion) > 64 {
		return errors.New("app_version too long")
	}
	return nil
}
