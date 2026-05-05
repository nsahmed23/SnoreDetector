// Package store wraps a Postgres pool with the queries the sync
// service needs. Hand-written for now; see ../../sqlc/ for the
// schema + queries that future regeneration will use.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// User mirrors the `users` row.
type User struct {
	ID             uuid.UUID
	AppleSubject   string
	Email          string
	IsPrivateEmail bool
	CreatedAt      time.Time
	LastSeenAt     time.Time
}

// UpsertUser inserts a new user keyed on apple_subject, or refreshes
// last_seen_at + email if the row already exists. Returns the row.
//
// Apple only returns `email` on the user's first sign-in, so we don't
// overwrite a non-null email with null.
func (s *Store) UpsertUser(
	ctx context.Context,
	appleSubject, email string,
	isPrivateEmail bool,
) (*User, error) {
	if appleSubject == "" {
		return nil, errors.New("store: apple_subject is required")
	}
	const q = `
INSERT INTO users (apple_subject, email, is_private_email)
VALUES ($1, NULLIF($2, ''), $3)
ON CONFLICT (apple_subject) DO UPDATE SET
    last_seen_at = NOW(),
    email = COALESCE(EXCLUDED.email, users.email),
    is_private_email = EXCLUDED.is_private_email
RETURNING id, apple_subject, COALESCE(email, ''), is_private_email,
          created_at, last_seen_at;
`
	row := s.pool.QueryRow(ctx, q, appleSubject, email, isPrivateEmail)
	u := &User{}
	if err := row.Scan(
		&u.ID, &u.AppleSubject, &u.Email, &u.IsPrivateEmail,
		&u.CreatedAt, &u.LastSeenAt,
	); err != nil {
		return nil, fmt.Errorf("store: upsert user: %w", err)
	}
	return u, nil
}

// SnoreEvent mirrors the `snore_events` row.
type SnoreEvent struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	ClientEventID string
	StartedAt     time.Time
	DurationMS    int32
	AvgDB         float32
	SessionID     string
	ReceivedAt    time.Time
}

// InsertEvents bulk-inserts events for a user, ignoring duplicates
// keyed on (user_id, client_event_id). Returns the number of rows
// actually inserted (i.e. excluding silently-deduped rows).
func (s *Store) InsertEvents(ctx context.Context, userID uuid.UUID, evs []SnoreEvent) (int, error) {
	if len(evs) == 0 {
		return 0, nil
	}
	const q = `
INSERT INTO snore_events
    (user_id, client_event_id, started_at, duration_ms, avg_db, session_id)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
ON CONFLICT (user_id, client_event_id) DO NOTHING;
`
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted int
	for _, e := range evs {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(ctx, q,
			userID, e.ClientEventID, e.StartedAt, e.DurationMS, e.AvgDB, e.SessionID,
		)
		if err != nil {
			return 0, fmt.Errorf("store: insert event %q: %w", e.ClientEventID, err)
		}
		inserted += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return inserted, nil
}

// ListEvents returns events for a user, optionally filtered by
// `received_at > since`, capped at `limit`. Ordered by received_at
// ascending so cursor-based pagination is straightforward.
func (s *Store) ListEvents(
	ctx context.Context,
	userID uuid.UUID,
	since time.Time,
	limit int,
) ([]SnoreEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	const q = `
SELECT id, user_id, client_event_id, started_at, duration_ms, avg_db,
       COALESCE(session_id, ''), received_at
FROM snore_events
WHERE user_id = $1 AND received_at > $2
ORDER BY received_at ASC
LIMIT $3;
`
	rows, err := s.pool.Query(ctx, q, userID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list events: %w", err)
	}
	defer rows.Close()

	var out []SnoreEvent
	for rows.Next() {
		var e SnoreEvent
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.ClientEventID, &e.StartedAt,
			&e.DurationMS, &e.AvgDB, &e.SessionID, &e.ReceivedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Ping verifies connectivity. Used by /healthz.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	return conn.Ping(ctx)
}

// Close drains the underlying pool.
func (s *Store) Close() { s.pool.Close() }

// Connect opens a pool against the given URL.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("store: parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	return pool, nil
}

// AcquireConn returns a single non-pooled connection (used by the
// migration runner, which expects pgx.Conn rather than a pool).
func AcquireConn(ctx context.Context, url string) (*pgx.Conn, error) {
	return pgx.Connect(ctx, url)
}
