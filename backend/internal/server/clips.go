package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/auth"
	"github.com/nsahmed23/SnoreDetector/backend/internal/blobstore"
	"github.com/nsahmed23/SnoreDetector/backend/internal/httpkit"
	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

const (
	defaultClipLimit = 100
	// Hard upper bound on the multipart body envelope. The clip body
	// itself is capped by AudioMaxClipBytes; this bound keeps the
	// metadata part + multipart overhead from blowing up memory.
	multipartOverhead = 64 * 1024
)

type clipsHandler struct {
	deps Deps
}

type clipMetadataRequest struct {
	ClientClipID  string    `json:"client_clip_id"`
	ClientEventID string    `json:"client_event_id,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int32     `json:"duration_ms"`
	AvgDB         *float32  `json:"avg_db,omitempty"`
	SHA256        string    `json:"sha256"`
}

type postClipResponse struct {
	ClipID     string    `json:"clip_id"`
	Created    bool      `json:"created"`
	ObjectKey  string    `json:"object_key"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type clipDTO struct {
	ClipID        string    `json:"clip_id"`
	SessionID     string    `json:"session_id,omitempty"`
	ClientClipID  string    `json:"client_clip_id"`
	ClientEventID string    `json:"client_event_id,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int32     `json:"duration_ms"`
	AvgDB         *float32  `json:"avg_db,omitempty"`
	ContentType   string    `json:"content_type"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	UploadedAt    time.Time `json:"uploaded_at"`
}

type listClipsResponse struct {
	Clips      []clipDTO `json:"clips"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// create handles POST /audio/clips. Body is multipart/form-data with
// two parts:
//
//	"metadata": JSON of clipMetadataRequest
//	"file":     binary audio body (≤ AudioMaxClipBytes)
//
// On idempotent retry of the same client_clip_id we return the
// existing row + 200 and do NOT overwrite the stored object.
func (h *clipsHandler) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.deps.BlobStore == nil {
		httpkit.Error(w, http.StatusInternalServerError, "blob store not configured")
		return
	}

	maxBytes := h.deps.AudioMaxClipBytes
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024
	}
	// Cap the *entire* multipart body. The +overhead lets the
	// metadata part + multipart boundaries through without making
	// the per-clip cap fuzzy from the caller's POV.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+multipartOverhead)

	mediaType, params, err := parseMultipartContentType(r.Header.Get("Content-Type"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if mediaType != "multipart/form-data" {
		httpkit.Error(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}
	boundary := params["boundary"]
	if boundary == "" {
		httpkit.Error(w, http.StatusBadRequest, "multipart boundary missing")
		return
	}

	mr := multipart.NewReader(r.Body, boundary)

	var meta clipMetadataRequest
	var (
		fileBytes      []byte
		filePartCT     string
		gotMetaPart    bool
		gotFilePart    bool
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// http.MaxBytesReader surfaces oversize-body via this path.
			if isMaxBytesError(err) {
				httpkit.Error(w, http.StatusRequestEntityTooLarge, "audio clip too large")
				return
			}
			httpkit.Error(w, http.StatusBadRequest, "invalid multipart body")
			return
		}
		switch part.FormName() {
		case "metadata":
			if gotMetaPart {
				httpkit.Error(w, http.StatusBadRequest, "duplicate 'metadata' part")
				_ = part.Close()
				return
			}
			gotMetaPart = true
			if err := json.NewDecoder(io.LimitReader(part, 16*1024)).Decode(&meta); err != nil {
				_ = part.Close()
				httpkit.Error(w, http.StatusBadRequest, "invalid metadata JSON")
				return
			}
			_ = part.Close()
		case "file":
			if gotFilePart {
				httpkit.Error(w, http.StatusBadRequest, "duplicate 'file' part")
				_ = part.Close()
				return
			}
			gotFilePart = true
			filePartCT = strings.TrimSpace(part.Header.Get("Content-Type"))
			// Read the body (capped by MaxBytesReader on r.Body); we
			// hash + sniff + write to the blobstore from the same buffer.
			fileBytes, err = io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				if isMaxBytesError(err) {
					httpkit.Error(w, http.StatusRequestEntityTooLarge, "audio clip too large")
					return
				}
				httpkit.Error(w, http.StatusBadRequest, "failed to read audio body")
				return
			}
		default:
			// Unknown parts are ignored; clients may add forward-compat
			// fields. Drain the part so multipart.Reader can advance.
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
		}
	}
	if !gotMetaPart {
		httpkit.Error(w, http.StatusBadRequest, "missing 'metadata' part")
		return
	}
	if !gotFilePart {
		httpkit.Error(w, http.StatusBadRequest, "missing 'file' part")
		return
	}
	if int64(len(fileBytes)) > maxBytes {
		httpkit.Error(w, http.StatusRequestEntityTooLarge, "audio clip too large")
		return
	}
	if len(fileBytes) == 0 {
		httpkit.Error(w, http.StatusBadRequest, "audio body is empty")
		return
	}
	if err := validateClipMetadata(meta); err != nil {
		httpkit.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// MIME allowlist. We require BOTH the part's declared Content-Type
	// AND the sniffed body type to be on the allowlist — the declared
	// type can be spoofed by a malicious client; the sniffed type can
	// be spoofed by a hand-crafted file header. Demanding both is
	// strictly better than either alone.
	allowed := h.deps.AudioAllowedMIME
	if len(allowed) == 0 {
		allowed = []string{"audio/m4a", "audio/mp4", "audio/wav", "audio/aac"}
	}
	if !mimeAllowed(filePartCT, allowed) {
		httpkit.Error(w, http.StatusUnsupportedMediaType,
			fmt.Sprintf("declared content_type %q not allowed", filePartCT))
		return
	}
	sniffed := http.DetectContentType(fileBytes)
	// http.DetectContentType returns the bare MIME with no parameters;
	// our allowlist matches the same shape.
	sniffedBare := strings.TrimSpace(strings.SplitN(sniffed, ";", 2)[0])
	if !mimeAllowed(sniffedBare, allowed) {
		httpkit.Error(w, http.StatusUnsupportedMediaType,
			fmt.Sprintf("sniffed content_type %q not allowed", sniffedBare))
		return
	}

	// Verify the sha256 the client claimed matches the bytes we received.
	hash := sha256.Sum256(fileBytes)
	gotSHA := hex.EncodeToString(hash[:])
	if !strings.EqualFold(gotSHA, meta.SHA256) {
		httpkit.Error(w, http.StatusBadRequest, "sha256 mismatch between metadata and body")
		return
	}

	var sessionPtr *uuid.UUID
	if meta.SessionID != "" {
		sid, err := uuid.Parse(meta.SessionID)
		if err != nil {
			httpkit.Error(w, http.StatusBadRequest, "invalid session_id (want UUID)")
			return
		}
		sessionPtr = &sid
	}

	// object_key is server-controlled. UUID v4 is enough — the user
	// can't influence the key, so the threat-model entry "no
	// user-controlled keys" holds regardless of what's in the metadata.
	objectKey := "clips/" + uid.String() + "/" + uuid.New().String()

	// Persist the metadata first so a crash between Put and the DB
	// insert can't leak an orphan blob without a row pointing at it.
	// Then write the object. Idempotency note: if the row already
	// exists for (user_id, client_clip_id) we return the existing one
	// and skip the blobstore write entirely so we don't overwrite the
	// previously-confirmed bytes.
	row, created, err := h.deps.Store.UpsertAudioClip(r.Context(), uid, store.AudioClip{
		SessionID:     sessionPtr,
		ClientClipID:  meta.ClientClipID,
		ClientEventID: meta.ClientEventID,
		StartedAt:     meta.StartedAt,
		DurationMS:    meta.DurationMS,
		AvgDB:         meta.AvgDB,
		ContentType:   sniffedBare,
		SizeBytes:     int64(len(fileBytes)),
		SHA256:        gotSHA,
		ObjectKey:     objectKey,
	})
	if err != nil {
		h.deps.Logger.Error("upsert audio clip", "err", err.Error(), "user", uid.String())
		httpkit.Error(w, http.StatusInternalServerError, "failed to record clip")
		return
	}
	if created {
		// New row — write the object using the row's stored object_key.
		if err := h.deps.BlobStore.Put(r.Context(), row.ObjectKey, bytesReader(fileBytes), row.ContentType); err != nil {
			h.deps.Logger.Error("blobstore put", "err", err.Error(), "user", uid.String(), "key", row.ObjectKey)
			httpkit.Error(w, http.StatusInternalServerError, "failed to store clip")
			return
		}
	}
	httpkit.JSON(w, http.StatusOK, postClipResponse{
		ClipID:     row.ID.String(),
		Created:    created,
		ObjectKey:  row.ObjectKey,
		UploadedAt: row.UploadedAt,
	})
}

func (h *clipsHandler) list(w http.ResponseWriter, r *http.Request) {
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
	limit := defaultClipLimit
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 || n > 1000 {
			httpkit.Error(w, http.StatusBadRequest, "invalid 'limit' (1..1000)")
			return
		}
		limit = n
	}
	rows, err := h.deps.Store.ListAudioClips(r.Context(), uid, cursor, limit)
	if err != nil {
		h.deps.Logger.Error("list audio clips", "err", err.Error())
		httpkit.Error(w, http.StatusInternalServerError, "failed to list clips")
		return
	}
	out := make([]clipDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, audioClipToDTO(c))
	}
	resp := listClipsResponse{Clips: out}
	if len(rows) == limit && len(rows) > 0 {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeDescCursor(store.DescCursor{StartedAt: last.StartedAt, ID: last.ID})
	}
	httpkit.JSON(w, http.StatusOK, resp)
}

// download streams the raw audio bytes for one clip.
func (h *clipsHandler) download(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.deps.BlobStore == nil {
		httpkit.Error(w, http.StatusInternalServerError, "blob store not configured")
		return
	}
	clipID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	row, err := h.deps.Store.GetAudioClip(r.Context(), uid, clipID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpkit.Error(w, http.StatusNotFound, "clip not found")
			return
		}
		h.deps.Logger.Error("get audio clip", "err", err.Error())
		httpkit.Error(w, http.StatusInternalServerError, "failed to load clip")
		return
	}
	rc, meta, err := h.deps.BlobStore.Get(r.Context(), row.ObjectKey)
	if err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			// Row exists but object is gone: expose as 404 to the user
			// and log loud so ops sees the orphan.
			h.deps.Logger.Warn("clip row without object", "user", uid.String(), "clip", clipID.String(), "key", row.ObjectKey)
			httpkit.Error(w, http.StatusNotFound, "clip body unavailable")
			return
		}
		h.deps.Logger.Error("blobstore get", "err", err.Error())
		httpkit.Error(w, http.StatusInternalServerError, "failed to fetch clip")
		return
	}
	defer rc.Close()
	if meta != nil && meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	} else {
		w.Header().Set("Content-Type", row.ContentType)
	}
	if meta != nil && meta.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.SizeBytes, 10))
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(row.SizeBytes, 10))
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="snoreguard-clip-%s.bin"`, clipID.String()))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// delete soft-deletes the row and best-effort removes the object.
// We delete the metadata first so the audit trail (deleted_at) is
// recorded even if the object delete fails — the object is then
// reaped by future cleanup, but the user's "I deleted this" intent
// is durably persisted.
func (h *clipsHandler) delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpkit.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	clipID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpkit.Error(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	row, err := h.deps.Store.GetAudioClip(r.Context(), uid, clipID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpkit.Error(w, http.StatusNotFound, "clip not found")
			return
		}
		h.deps.Logger.Error("get audio clip", "err", err.Error())
		httpkit.Error(w, http.StatusInternalServerError, "failed to load clip")
		return
	}
	if err := h.deps.Store.SoftDeleteAudioClip(r.Context(), uid, clipID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpkit.Error(w, http.StatusNotFound, "clip not found")
			return
		}
		h.deps.Logger.Error("soft delete clip", "err", err.Error())
		httpkit.Error(w, http.StatusInternalServerError, "failed to delete clip")
		return
	}
	if h.deps.BlobStore != nil {
		if err := h.deps.BlobStore.Delete(r.Context(), row.ObjectKey); err != nil && !errors.Is(err, blobstore.ErrNotFound) {
			// Object orphaned; log but don't fail the request — the
			// user's intent is durable in the soft-deleted row.
			h.deps.Logger.Warn("blobstore delete after soft-delete",
				"err", err.Error(), "user", uid.String(),
				"clip", clipID.String(), "key", row.ObjectKey)
		}
	}
	httpkit.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func validateClipMetadata(m clipMetadataRequest) error {
	if m.ClientClipID == "" {
		return errors.New("client_clip_id is required")
	}
	if len(m.ClientClipID) > 128 {
		return errors.New("client_clip_id too long")
	}
	if m.StartedAt.IsZero() {
		return errors.New("started_at is required")
	}
	if m.DurationMS < 0 {
		return errors.New("duration_ms must be ≥ 0")
	}
	if m.DurationMS > int32(24*time.Hour/time.Millisecond) {
		return errors.New("duration_ms unreasonably large")
	}
	if m.SHA256 == "" {
		return errors.New("sha256 is required")
	}
	if len(m.SHA256) != 64 {
		return errors.New("sha256 must be 64 hex chars")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return errors.New("sha256 must be hex")
	}
	if len(m.ClientEventID) > 128 {
		return errors.New("client_event_id too long")
	}
	if m.AvgDB != nil {
		v := *m.AvgDB
		if v < 0 || v > 200 {
			return errors.New("avg_db out of range [0, 200]")
		}
	}
	return nil
}

func mimeAllowed(ct string, allowed []string) bool {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if ct == "" {
		return false
	}
	// Drop any parameters (charset=..., codecs=...).
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), ct) {
			return true
		}
	}
	return false
}

func parseMultipartContentType(ct string) (string, map[string]string, error) {
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", nil, fmt.Errorf("invalid Content-Type: %v", err)
	}
	return mediaType, params, nil
}

// bytesReader is a small wrapper that returns an *bytes.Reader. We
// could pass *bytes.Reader directly, but naming it makes the call
// site at the blobstore.Put boundary clearer.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// isMaxBytesError reports whether the error came from
// http.MaxBytesReader hitting its cap. The standard library exposes
// this via *http.MaxBytesError starting in Go 1.19.
func isMaxBytesError(err error) bool {
	var mb *http.MaxBytesError
	return errors.As(err, &mb)
}

// audioClipToDTO converts a store.AudioClip into the JSON shape both
// the list endpoint and the export endpoint return. Single source of
// truth so the two stays in sync.
func audioClipToDTO(c store.AudioClip) clipDTO {
	d := clipDTO{
		ClipID:        c.ID.String(),
		ClientClipID:  c.ClientClipID,
		ClientEventID: c.ClientEventID,
		StartedAt:     c.StartedAt.UTC(),
		DurationMS:    c.DurationMS,
		AvgDB:         c.AvgDB,
		ContentType:   c.ContentType,
		SizeBytes:     c.SizeBytes,
		SHA256:        c.SHA256,
		UploadedAt:    c.UploadedAt.UTC(),
	}
	if c.SessionID != nil {
		d.SessionID = c.SessionID.String()
	}
	return d
}

// recordingSessionToDTO is the export-side equivalent of audioClipToDTO.
func recordingSessionToDTO(s store.RecordingSession) sessionDTO {
	return sessionDTO{
		SessionID:       s.ID.String(),
		ClientSessionID: s.ClientSessionID,
		StartedAt:       s.StartedAt.UTC(),
		EndedAt:         s.EndedAt,
		DeviceName:      s.DeviceName,
		AppVersion:      s.AppVersion,
	}
}
