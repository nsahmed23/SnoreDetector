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

// ErrAlreadyRotated indicates a refresh-token row's `replaced_by` is
// already set — i.e. the caller is replaying a refresh token that was
// previously exchanged for a new one. Treated as theft by the server
// layer (entire family revoked).
var ErrAlreadyRotated = errors.New("store: refresh token already rotated")

// ErrRefreshNotFound indicates the requested refresh-token row does
// not exist (or has been deleted).
var ErrRefreshNotFound = errors.New("store: refresh token not found")

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

// Cursor identifies a position in the (received_at, id)-ordered event
// stream. The zero value (received_at = epoch, id = uuid.Nil) selects
// every row, since the row tuple comparison
//
//	(received_at, id) > ('epoch'::timestamptz, '00000000-0000-0000-0000-000000000000')
//
// is true for any real row.
type Cursor struct {
	ReceivedAt time.Time
	ID         uuid.UUID
}

// ZeroCursor returns the cursor that selects everything (the first page).
func ZeroCursor() Cursor { return Cursor{} }

// ListEvents returns events for a user with received_at strictly
// greater than the cursor (compound key (received_at, id)). Capped at
// `limit`. Ordered by received_at, id ascending so that
// cursor-based pagination is stable when many events share the same
// received_at (e.g. a single batched insert that uses NOW() once).
func (s *Store) ListEvents(
	ctx context.Context,
	userID uuid.UUID,
	cursor Cursor,
	limit int,
) ([]SnoreEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	const q = `
SELECT id, user_id, client_event_id, started_at, duration_ms, avg_db,
       COALESCE(session_id, ''), received_at
FROM snore_events
WHERE user_id = $1 AND (received_at, id) > ($2, $3)
ORDER BY received_at ASC, id ASC
LIMIT $4;
`
	rows, err := s.pool.Query(ctx, q, userID, cursor.ReceivedAt, cursor.ID, limit)
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

// RefreshToken mirrors the `refresh_tokens` row.
type RefreshToken struct {
	TokenID    uuid.UUID
	UserID     uuid.UUID
	FamilyID   uuid.UUID
	IssuedAt   time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	ReplacedBy *uuid.UUID
}

// RecordRefreshToken inserts a freshly issued refresh-token row.
func (s *Store) RecordRefreshToken(
	ctx context.Context,
	tokenID, userID, familyID uuid.UUID,
	expiresAt time.Time,
) error {
	const q = `
INSERT INTO refresh_tokens (token_id, user_id, family_id, expires_at)
VALUES ($1, $2, $3, $4);
`
	if _, err := s.pool.Exec(ctx, q, tokenID, userID, familyID, expiresAt); err != nil {
		return fmt.Errorf("store: record refresh token: %w", err)
	}
	return nil
}

// GetRefreshToken loads a refresh-token row by its token_id.
func (s *Store) GetRefreshToken(ctx context.Context, tokenID uuid.UUID) (*RefreshToken, error) {
	const q = `
SELECT token_id, user_id, family_id, issued_at, expires_at, revoked_at, replaced_by
FROM refresh_tokens
WHERE token_id = $1;
`
	row := s.pool.QueryRow(ctx, q, tokenID)
	rt := &RefreshToken{}
	if err := row.Scan(
		&rt.TokenID, &rt.UserID, &rt.FamilyID,
		&rt.IssuedAt, &rt.ExpiresAt, &rt.RevokedAt, &rt.ReplacedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRefreshNotFound
		}
		return nil, fmt.Errorf("store: get refresh token: %w", err)
	}
	return rt, nil
}

// ReplaceRefreshToken atomically rotates `oldID` into `newID`. In a
// single transaction it inserts the new row, then sets the old row's
// `replaced_by` to the new id. Returns ErrAlreadyRotated if the old
// row's `replaced_by` was already non-null at start of the transaction
// (the theft-detection signal).
func (s *Store) ReplaceRefreshToken(
	ctx context.Context,
	oldID, newID, userID, familyID uuid.UUID,
	expiresAt time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the old row so concurrent /auth/refresh calls serialize on
	// the same parent token — only one wins, the other observes
	// replaced_by != NULL and triggers theft detection.
	var (
		replacedBy *uuid.UUID
		revokedAt  *time.Time
	)
	err = tx.QueryRow(ctx,
		`SELECT replaced_by, revoked_at FROM refresh_tokens WHERE token_id = $1 FOR UPDATE`,
		oldID,
	).Scan(&replacedBy, &revokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRefreshNotFound
		}
		return fmt.Errorf("store: lock old token: %w", err)
	}
	if replacedBy != nil || revokedAt != nil {
		return ErrAlreadyRotated
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (token_id, user_id, family_id, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		newID, userID, familyID, expiresAt,
	); err != nil {
		return fmt.Errorf("store: insert new token: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET replaced_by = $1 WHERE token_id = $2`,
		newID, oldID,
	); err != nil {
		return fmt.Errorf("store: mark replaced_by: %w", err)
	}
	return tx.Commit(ctx)
}

// RevokeRefreshFamily marks every still-active refresh token in the
// given family as revoked. Used both at logout and after theft is
// detected. Idempotent.
func (s *Store) RevokeRefreshFamily(ctx context.Context, userID, familyID uuid.UUID) error {
	const q = `
UPDATE refresh_tokens
SET revoked_at = NOW()
WHERE user_id = $1 AND family_id = $2 AND revoked_at IS NULL;
`
	if _, err := s.pool.Exec(ctx, q, userID, familyID); err != nil {
		return fmt.Errorf("store: revoke refresh family: %w", err)
	}
	return nil
}

// RecordRevokedJTI records that a particular access-token JTI has been
// revoked (e.g. at logout). Idempotent — a duplicate insert is not an
// error.
func (s *Store) RecordRevokedJTI(ctx context.Context, jti string, userID uuid.UUID) error {
	const q = `
INSERT INTO revoked_jti (jti, user_id) VALUES ($1, $2)
ON CONFLICT (jti) DO NOTHING;
`
	if _, err := s.pool.Exec(ctx, q, jti, userID); err != nil {
		return fmt.Errorf("store: record revoked jti: %w", err)
	}
	return nil
}

// IsJTIRevoked returns true if the given access-token JTI has been
// recorded as revoked.
func (s *Store) IsJTIRevoked(ctx context.Context, jti string) (bool, error) {
	const q = `SELECT 1 FROM revoked_jti WHERE jti = $1;`
	var one int
	err := s.pool.QueryRow(ctx, q, jti).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("store: is jti revoked: %w", err)
	}
	return true, nil
}

// DailySummary is one row of a per-day aggregation, in the user's
// configured time zone (which the analytics service passes in).
type DailySummary struct {
	Day             time.Time
	EventCount      int64
	TotalDurationMS int64
	AvgDB           float64
	LongestMS       int32
}

// DailySummaries returns per-day aggregations between [start, end].
// `tz` is an IANA timezone name (e.g. "America/Los_Angeles") used to
// bucket events into local-day rows. Pass "UTC" for UTC days.
func (s *Store) DailySummaries(
	ctx context.Context,
	userID uuid.UUID,
	tz string,
	start, end time.Time,
) ([]DailySummary, error) {
	if tz == "" {
		tz = "UTC"
	}
	const q = `
SELECT (started_at AT TIME ZONE $4)::date AS day,
       COUNT(*)::bigint        AS event_count,
       COALESCE(SUM(duration_ms), 0)::bigint AS total_ms,
       COALESCE(AVG(avg_db), 0)::double precision AS avg_db,
       COALESCE(MAX(duration_ms), 0)::int AS longest_ms
FROM snore_events
WHERE user_id = $1 AND started_at >= $2 AND started_at < $3
GROUP BY day
ORDER BY day ASC;
`
	rows, err := s.pool.Query(ctx, q, userID, start, end, tz)
	if err != nil {
		return nil, fmt.Errorf("store: daily summaries: %w", err)
	}
	defer rows.Close()

	var out []DailySummary
	for rows.Next() {
		var d DailySummary
		if err := rows.Scan(&d.Day, &d.EventCount, &d.TotalDurationMS, &d.AvgDB, &d.LongestMS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Totals is the all-time-or-since rollup used by the analytics
// /totals endpoint.
type Totals struct {
	EventCount      int64
	TotalDurationMS int64
	AvgDB           float64
	LongestMS       int32
	FirstEventAt    *time.Time
	LastEventAt     *time.Time
}

// TotalsSince returns aggregate stats for all events with started_at
// after `since`. Pass time.Time{} for "all time".
func (s *Store) TotalsSince(
	ctx context.Context,
	userID uuid.UUID,
	since time.Time,
) (*Totals, error) {
	const q = `
SELECT COUNT(*)::bigint                              AS event_count,
       COALESCE(SUM(duration_ms), 0)::bigint         AS total_ms,
       COALESCE(AVG(avg_db), 0)::double precision    AS avg_db,
       COALESCE(MAX(duration_ms), 0)::int            AS longest_ms,
       MIN(started_at)                               AS first_at,
       MAX(started_at)                               AS last_at
FROM snore_events
WHERE user_id = $1 AND started_at >= $2;
`
	row := s.pool.QueryRow(ctx, q, userID, since)
	t := &Totals{}
	var first, last *time.Time
	if err := row.Scan(&t.EventCount, &t.TotalDurationMS, &t.AvgDB, &t.LongestMS, &first, &last); err != nil {
		return nil, fmt.Errorf("store: totals: %w", err)
	}
	t.FirstEventAt = first
	t.LastEventAt = last
	return t, nil
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
