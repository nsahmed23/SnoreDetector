-- Queries used by the sync service. Used as input to sqlc; the
-- hand-written equivalents live in internal/store/store.go for now.

-- name: UpsertUser :one
INSERT INTO users (apple_subject, email, is_private_email)
VALUES ($1, NULLIF($2, ''), $3)
ON CONFLICT (apple_subject) DO UPDATE SET
    last_seen_at = NOW(),
    email = COALESCE(EXCLUDED.email, users.email),
    is_private_email = EXCLUDED.is_private_email
RETURNING id, apple_subject, COALESCE(email, '')::TEXT AS email,
          is_private_email, created_at, last_seen_at;

-- name: InsertSnoreEvent :execrows
INSERT INTO snore_events
    (user_id, client_event_id, started_at, duration_ms, avg_db, session_id)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
ON CONFLICT (user_id, client_event_id) DO NOTHING;

-- name: ListSnoreEvents :many
SELECT id, user_id, client_event_id, started_at, duration_ms, avg_db,
       COALESCE(session_id, '')::TEXT AS session_id, received_at
FROM snore_events
WHERE user_id = $1 AND received_at > $2
ORDER BY received_at ASC
LIMIT $3;
