-- 001_init.up.sql
-- Initial schema for the SnoreGuard sync service.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    apple_subject   TEXT NOT NULL UNIQUE,
    email           TEXT,
    is_private_email BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Snore events recorded on-device and pushed for cross-device sync.
-- Schema mirrors the Rust core's SgEvent + a session reference.
CREATE TABLE snore_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Stable client-side identifier so retries are idempotent.
    client_event_id TEXT NOT NULL,
    -- When the event started, in the device's local clock.
    started_at      TIMESTAMPTZ NOT NULL,
    duration_ms     INTEGER NOT NULL CHECK (duration_ms >= 0),
    avg_db          REAL NOT NULL,
    -- For debugging: which session/recording this came from.
    session_id      TEXT,
    -- When the server received it (for incremental sync).
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, client_event_id)
);

CREATE INDEX snore_events_user_received_idx
    ON snore_events (user_id, received_at);
CREATE INDEX snore_events_user_started_idx
    ON snore_events (user_id, started_at);
