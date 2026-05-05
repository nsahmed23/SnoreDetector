-- This file is the canonical schema for sqlc codegen. It must stay in
-- lock-step with backend/migrations/*.up.sql.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    apple_subject   TEXT NOT NULL UNIQUE,
    email           TEXT,
    is_private_email BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE snore_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_event_id TEXT NOT NULL,
    started_at      TIMESTAMPTZ NOT NULL,
    duration_ms     INTEGER NOT NULL CHECK (duration_ms >= 0),
    avg_db          REAL NOT NULL,
    session_id      TEXT,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, client_event_id)
);

CREATE INDEX snore_events_user_received_idx
    ON snore_events (user_id, received_at);
CREATE INDEX snore_events_user_started_idx
    ON snore_events (user_id, started_at);
