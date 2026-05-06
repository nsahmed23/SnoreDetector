-- 007_audio_clips.up.sql
-- Raw audio clips uploaded by the iOS app. Object bytes live in the
-- configured blobstore (filesystem for dev; GCS/S3 follow-up); this
-- table holds metadata + the object_key pointer. (user_id,
-- client_clip_id) is the idempotency key for retried uploads.
--
-- session_id is nullable + ON DELETE SET NULL so deleting a session
-- never orphans a clip's object — the clip survives so the user can
-- still listen to it / delete it on their own terms.
--
-- Soft-delete (deleted_at) is the metadata-side delete; the object
-- itself is removed best-effort by the handler. The audit trail
-- (the row with deleted_at != NULL) survives forever for ops review.
CREATE TABLE audio_clips (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id        UUID REFERENCES recording_sessions(id) ON DELETE SET NULL,
    client_clip_id    TEXT NOT NULL,
    client_event_id   TEXT,
    started_at        TIMESTAMPTZ NOT NULL,
    duration_ms       INTEGER NOT NULL CHECK (duration_ms >= 0),
    avg_db            REAL,
    content_type      TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL CHECK (size_bytes > 0),
    sha256            TEXT NOT NULL,
    object_key        TEXT NOT NULL,
    uploaded_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ,
    UNIQUE (user_id, client_clip_id)
);
-- "Newest-first list of my clips" — the most common read.
CREATE INDEX audio_clips_user_started_idx       ON audio_clips (user_id, started_at DESC);
-- "Clips for a given session" — used by future "session detail" views.
CREATE INDEX audio_clips_user_session_idx       ON audio_clips (user_id, session_id);
-- Partial index on the active (non-soft-deleted) set so the list path
-- doesn't have to scan tombstones.
CREATE INDEX audio_clips_user_active_idx        ON audio_clips (user_id) WHERE deleted_at IS NULL;
