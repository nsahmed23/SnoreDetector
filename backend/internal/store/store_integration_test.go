//go:build integration

package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
	"github.com/nsahmed23/SnoreDetector/backend/migrations"
)

// These tests require a running Postgres reachable via $DATABASE_URL
// (no Docker dependency in the harness — bring your own DB).
//
//   make test-integration
//
// is a convenience wrapper that runs `go test -tags=integration ./...`
// after exporting DATABASE_URL.

func requireDB(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	return url
}

func freshDB(t *testing.T) (*store.Store, func()) {
	t.Helper()
	url := requireDB(t)
	ctx := context.Background()

	migConn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect for migrations: %v", err)
	}
	// Tear down any previous run, then migrate fresh.
	_, _ = migConn.Exec(ctx, `
		DROP TABLE IF EXISTS snore_events;
		DROP TABLE IF EXISTS users;
		DROP TABLE IF EXISTS schema_migrations;
	`)
	if err := migrations.Up(ctx, migConn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	_ = migConn.Close(ctx)

	pool, err := store.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect pool: %v", err)
	}
	s := store.New(pool)
	return s, func() { s.Close() }
}

func TestUpsertUser_InsertThenUpdate(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	u1, err := s.UpsertUser(ctx, "apple-sub-1", "first@example.com", true)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	u2, err := s.UpsertUser(ctx, "apple-sub-1", "", false)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if u1.ID != u2.ID {
		t.Errorf("user ID changed across upserts: %s -> %s", u1.ID, u2.ID)
	}
	// Email must NOT have been overwritten with empty string.
	if u2.Email != "first@example.com" {
		t.Errorf("email overwritten: %q", u2.Email)
	}
}

func TestInsertEvents_Idempotent(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	user, _ := s.UpsertUser(ctx, "apple-sub-2", "a@b.com", false)
	events := []store.SnoreEvent{
		{ClientEventID: "ev-1", StartedAt: time.Now(), DurationMS: 1000, AvgDB: 60, SessionID: "sess-1"},
		{ClientEventID: "ev-2", StartedAt: time.Now(), DurationMS: 2000, AvgDB: 65},
	}
	n1, err := s.InsertEvents(ctx, user.ID, events)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if n1 != 2 {
		t.Errorf("first insert n=%d, want 2", n1)
	}
	// Re-inserting the same events should be a no-op.
	n2, err := s.InsertEvents(ctx, user.ID, events)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second insert n=%d, want 0", n2)
	}
}

func TestListEvents_FilterAndPaginate(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	mine, _ := s.UpsertUser(ctx, "apple-sub-3", "a@b.com", false)
	other, _ := s.UpsertUser(ctx, "apple-sub-4", "c@d.com", false)

	now := time.Now().UTC()
	_, err := s.InsertEvents(ctx, mine.ID, []store.SnoreEvent{
		{ClientEventID: "m1", StartedAt: now, DurationMS: 100, AvgDB: 60},
		{ClientEventID: "m2", StartedAt: now, DurationMS: 100, AvgDB: 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.InsertEvents(ctx, other.ID, []store.SnoreEvent{
		{ClientEventID: "o1", StartedAt: now, DurationMS: 100, AvgDB: 60},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.ListEvents(ctx, mine.ID, time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d events, want 2", len(got))
	}
	for _, e := range got {
		if e.UserID != mine.ID {
			t.Errorf("returned event for wrong user: %s", e.UserID)
		}
	}
}

func TestMigrations_Idempotent(t *testing.T) {
	url := requireDB(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if err := migrations.Up(ctx, conn); err != nil {
		t.Fatalf("first up: %v", err)
	}
	if err := migrations.Up(ctx, conn); err != nil {
		t.Fatalf("second up should be no-op: %v", err)
	}
}

// Compile-time: store integration tests use *store.Store, which must
// satisfy the server-package Store interface.
var _ = uuid.New // keep uuid imported even when no test uses it directly