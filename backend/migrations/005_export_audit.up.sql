-- 005_export_audit.up.sql
-- Per-user export audit log. Powers the per-user export budget
-- (EXPORT_BUDGET_PER_DAY) and gives an after-the-fact paper trail
-- of who downloaded what. We index on (user_id, requested_at DESC)
-- so the budget-check query — "how many exports has this user run
-- in the last 24h?" — is a single index scan.
CREATE TABLE export_audit (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    format       TEXT NOT NULL,           -- 'csv' or 'json'
    bytes_sent   BIGINT,                  -- nullable; populated on success
    status_code  INTEGER NOT NULL         -- final HTTP status
);
CREATE INDEX export_audit_user_recent_idx ON export_audit (user_id, requested_at DESC);
