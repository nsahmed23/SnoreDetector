-- 002_refresh_tokens.up.sql
-- Refresh-token table for rotation + theft detection.
--
-- Each refresh-token row represents one issued refresh token. On
-- successful /auth/refresh we insert a new row and set the previous
-- row's `replaced_by` to the new row's `token_id`. Reuse of an already-
-- replaced token is treated as theft and triggers a family-wide revoke
-- via the `family_id` column.
CREATE TABLE refresh_tokens (
    token_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id   UUID NOT NULL,
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    replaced_by UUID REFERENCES refresh_tokens(token_id) ON DELETE SET NULL
);

CREATE INDEX refresh_tokens_user_family_idx
    ON refresh_tokens (user_id, family_id);

CREATE INDEX refresh_tokens_user_idx
    ON refresh_tokens (user_id);
