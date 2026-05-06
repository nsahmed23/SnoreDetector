package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when a row lookup (e.g. GetAudioClip) finds
// nothing. Distinct from pgx.ErrNoRows so callers don't have to depend
// on the driver package.
var ErrNotFound = errors.New("store: not found")

// RecordingSession mirrors the recording_sessions row.
type RecordingSession struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	ClientSessionID string
	StartedAt       time.Time
	EndedAt         *time.Time
	DeviceName      string
	AppVersion      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// UpsertSession creates a session keyed on (user_id, client_session_id),
// or updates the existing row's mutable fields (ended_at, device_name,
// app_version) using COALESCE so a partial update never blanks an
// already-set field. The bool return is true when a fresh row was
// inserted, false when an existing one was updated.
//
// The (xmax = 0) trick: in Postgres, xmax is the deleting/updating
// transaction id of a tuple. After a successful INSERT, xmax = 0;
// after an UPDATE (including ON CONFLICT DO UPDATE), xmax is set.
// So `xmax = 0` on the returned row distinguishes insert from update
// without a separate roundtrip.
func (s *Store) UpsertSession(ctx context.Context, userID uuid.UUID, in RecordingSession) (*RecordingSession, bool, error) {
	if in.ClientSessionID == "" {
		return nil, false, errors.New("store: client_session_id is required")
	}
	const q = `
INSERT INTO recording_sessions
    (user_id, client_session_id, started_at, ended_at, device_name, app_version)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''))
ON CONFLICT (user_id, client_session_id) DO UPDATE SET
    ended_at    = COALESCE(EXCLUDED.ended_at,    recording_sessions.ended_at),
    device_name = COALESCE(EXCLUDED.device_name, recording_sessions.device_name),
    app_version = COALESCE(EXCLUDED.app_version, recording_sessions.app_version),
    updated_at  = NOW()
RETURNING id, user_id, client_session_id, started_at, ended_at,
          COALESCE(device_name, ''), COALESCE(app_version, ''),
          created_at, updated_at, (xmax = 0) AS inserted;
`
	row := s.pool.QueryRow(ctx, q,
		userID, in.ClientSessionID, in.StartedAt, in.EndedAt, in.DeviceName, in.AppVersion,
	)
	out := &RecordingSession{}
	var created bool
	if err := row.Scan(
		&out.ID, &out.UserID, &out.ClientSessionID, &out.StartedAt, &out.EndedAt,
		&out.DeviceName, &out.AppVersion, &out.CreatedAt, &out.UpdatedAt, &created,
	); err != nil {
		return nil, false, fmt.Errorf("store: upsert session: %w", err)
	}
	return out, created, nil
}

// DescCursor identifies a position in a (started_at, id)-ordered DESC
// stream. The zero value selects everything (NULL is treated as
// infinity in the comparison below). Encoded the same way as
// store.Cursor on the wire.
type DescCursor struct {
	StartedAt time.Time
	ID        uuid.UUID
}

// ZeroDescCursor returns the cursor that selects the first DESC page —
// the row tuple comparison is "(s.started_at, s.id) < (cursor.At,
// cursor.ID)" which we want to be vacuously true on the first call.
// The trick is to use NULL in SQL when both fields are zero so the
// query branches on COALESCE; see the WHERE clauses below.
func ZeroDescCursor() DescCursor { return DescCursor{} }

// ListSessions returns sessions newest-first, paginated by the
// compound (started_at DESC, id DESC) cursor. Same opaque-base64
// encoding lives in the server package; the store only sees the
// decoded tuple.
func (s *Store) ListSessions(ctx context.Context, userID uuid.UUID, cursor DescCursor, limit int) ([]RecordingSession, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	const q = `
SELECT id, user_id, client_session_id, started_at, ended_at,
       COALESCE(device_name, ''), COALESCE(app_version, ''),
       created_at, updated_at
FROM recording_sessions
WHERE user_id = $1
  AND ($2::timestamptz IS NULL
       OR (started_at, id) < ($2::timestamptz, $3::uuid))
ORDER BY started_at DESC, id DESC
LIMIT $4;
`
	var startedAtArg any
	var idArg any
	if cursor.StartedAt.IsZero() && cursor.ID == uuid.Nil {
		startedAtArg = nil
		idArg = nil
	} else {
		startedAtArg = cursor.StartedAt
		idArg = cursor.ID
	}
	rows, err := s.pool.Query(ctx, q, userID, startedAtArg, idArg, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()
	var out []RecordingSession
	for rows.Next() {
		var r RecordingSession
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.ClientSessionID, &r.StartedAt, &r.EndedAt,
			&r.DeviceName, &r.AppVersion, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AudioClip mirrors the audio_clips row.
type AudioClip struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	SessionID     *uuid.UUID
	ClientClipID  string
	ClientEventID string
	StartedAt     time.Time
	DurationMS    int32
	AvgDB         *float32
	ContentType   string
	SizeBytes     int64
	SHA256        string
	ObjectKey     string
	UploadedAt    time.Time
	DeletedAt     *time.Time
}

// UpsertAudioClip inserts a new clip or returns the existing row if
// (user_id, client_clip_id) already exists. We do NOT overwrite the
// stored object_key / sha256 / content_type on conflict — the first
// successful upload wins so a retried multipart with a different body
// can't silently swap the bytes the user previously confirmed.
//
// Created bool is true iff a fresh row was inserted (xmax-trick same
// as UpsertSession).
func (s *Store) UpsertAudioClip(ctx context.Context, userID uuid.UUID, in AudioClip) (*AudioClip, bool, error) {
	if in.ClientClipID == "" {
		return nil, false, errors.New("store: client_clip_id is required")
	}
	if in.ObjectKey == "" {
		return nil, false, errors.New("store: object_key is required")
	}
	if in.SHA256 == "" {
		return nil, false, errors.New("store: sha256 is required")
	}
	if in.ContentType == "" {
		return nil, false, errors.New("store: content_type is required")
	}
	if in.SizeBytes <= 0 {
		return nil, false, errors.New("store: size_bytes must be > 0")
	}
	const q = `
INSERT INTO audio_clips
    (user_id, session_id, client_clip_id, client_event_id, started_at,
     duration_ms, avg_db, content_type, size_bytes, sha256, object_key)
VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (user_id, client_clip_id) DO UPDATE SET
    -- Touch a no-op so RETURNING gives us the existing row's values.
    -- We deliberately do NOT mutate any of the ingestion fields.
    client_clip_id = EXCLUDED.client_clip_id
RETURNING id, user_id, session_id, client_clip_id, COALESCE(client_event_id, ''),
          started_at, duration_ms, avg_db, content_type, size_bytes,
          sha256, object_key, uploaded_at, deleted_at,
          (xmax = 0) AS inserted;
`
	row := s.pool.QueryRow(ctx, q,
		userID, in.SessionID, in.ClientClipID, in.ClientEventID, in.StartedAt,
		in.DurationMS, in.AvgDB, in.ContentType, in.SizeBytes, in.SHA256, in.ObjectKey,
	)
	out := &AudioClip{}
	var created bool
	if err := row.Scan(
		&out.ID, &out.UserID, &out.SessionID, &out.ClientClipID, &out.ClientEventID,
		&out.StartedAt, &out.DurationMS, &out.AvgDB, &out.ContentType, &out.SizeBytes,
		&out.SHA256, &out.ObjectKey, &out.UploadedAt, &out.DeletedAt, &created,
	); err != nil {
		return nil, false, fmt.Errorf("store: upsert audio clip: %w", err)
	}
	return out, created, nil
}

// ListAudioClips returns the user's non-soft-deleted clips, newest-
// first, paginated by the same (started_at DESC, id DESC) compound
// cursor as ListSessions.
func (s *Store) ListAudioClips(ctx context.Context, userID uuid.UUID, cursor DescCursor, limit int) ([]AudioClip, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	const q = `
SELECT id, user_id, session_id, client_clip_id, COALESCE(client_event_id, ''),
       started_at, duration_ms, avg_db, content_type, size_bytes,
       sha256, object_key, uploaded_at, deleted_at
FROM audio_clips
WHERE user_id = $1
  AND deleted_at IS NULL
  AND ($2::timestamptz IS NULL
       OR (started_at, id) < ($2::timestamptz, $3::uuid))
ORDER BY started_at DESC, id DESC
LIMIT $4;
`
	var startedAtArg any
	var idArg any
	if cursor.StartedAt.IsZero() && cursor.ID == uuid.Nil {
		startedAtArg = nil
		idArg = nil
	} else {
		startedAtArg = cursor.StartedAt
		idArg = cursor.ID
	}
	rows, err := s.pool.Query(ctx, q, userID, startedAtArg, idArg, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audio clips: %w", err)
	}
	defer rows.Close()
	var out []AudioClip
	for rows.Next() {
		var c AudioClip
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.SessionID, &c.ClientClipID, &c.ClientEventID,
			&c.StartedAt, &c.DurationMS, &c.AvgDB, &c.ContentType, &c.SizeBytes,
			&c.SHA256, &c.ObjectKey, &c.UploadedAt, &c.DeletedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetAudioClip loads a single clip scoped to the user. Returns
// ErrNotFound for missing rows, cross-user reads, AND soft-deleted
// rows — callers shouldn't be able to tell the difference between
// "never existed" and "deleted" without auditor-level access.
func (s *Store) GetAudioClip(ctx context.Context, userID, clipID uuid.UUID) (*AudioClip, error) {
	const q = `
SELECT id, user_id, session_id, client_clip_id, COALESCE(client_event_id, ''),
       started_at, duration_ms, avg_db, content_type, size_bytes,
       sha256, object_key, uploaded_at, deleted_at
FROM audio_clips
WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL;
`
	row := s.pool.QueryRow(ctx, q, userID, clipID)
	c := &AudioClip{}
	if err := row.Scan(
		&c.ID, &c.UserID, &c.SessionID, &c.ClientClipID, &c.ClientEventID,
		&c.StartedAt, &c.DurationMS, &c.AvgDB, &c.ContentType, &c.SizeBytes,
		&c.SHA256, &c.ObjectKey, &c.UploadedAt, &c.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get audio clip: %w", err)
	}
	return c, nil
}

// SoftDeleteAudioClip stamps deleted_at on the clip if it's still
// active. Idempotent in the sense that a second call on an already-
// deleted clip returns ErrNotFound (the row no longer matches the
// "not yet deleted" predicate). Returns ErrNotFound on cross-user
// access or missing IDs too.
func (s *Store) SoftDeleteAudioClip(ctx context.Context, userID, clipID uuid.UUID) error {
	const q = `
UPDATE audio_clips
SET deleted_at = NOW()
WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL;
`
	tag, err := s.pool.Exec(ctx, q, userID, clipID)
	if err != nil {
		return fmt.Errorf("store: soft delete audio clip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
