-- 006_recording_sessions.up.sql
-- Recording sessions group together a single device's recording run
-- (start to stop) so the iOS app can attach raw audio clips and snore
-- events to a coherent unit. The (user_id, client_session_id) unique
-- index makes the upsert path idempotent: a client retry of the same
-- "started a session" call returns the existing row.
CREATE TABLE recording_sessions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_session_id  TEXT NOT NULL,
    started_at         TIMESTAMPTZ NOT NULL,
    ended_at           TIMESTAMPTZ,
    device_name        TEXT,
    app_version        TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, client_session_id)
);
-- Sessions are listed newest-first; the compound index on
-- (user_id, started_at DESC) keeps that scan cheap.
CREATE INDEX recording_sessions_user_started_idx
    ON recording_sessions (user_id, started_at DESC);
