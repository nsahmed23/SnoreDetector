-- 003_revoked_jti.up.sql
-- Revoked access-token JTIs. Consulted by the auth middleware on every
-- request so that /auth/logout can take effect immediately rather than
-- waiting for the access token's natural expiry.
CREATE TABLE revoked_jti (
    jti         TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    revoked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX revoked_jti_user_idx ON revoked_jti (user_id);
