//go:build integration

package store_test

import (
	"context"
	"errors"
	"fmt"
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
		DROP TABLE IF EXISTS audio_clips;
		DROP TABLE IF EXISTS recording_sessions;
		DROP TABLE IF EXISTS export_audit;
		DROP TABLE IF EXISTS revoked_jti;
		DROP TABLE IF EXISTS refresh_tokens;
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

	got, err := s.ListEvents(ctx, mine.ID, store.ZeroCursor(), 100)
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

// TestListEvents_PaginatesAcrossSharedReceivedAt is the regression
// test for the data-loss bug where the old single-timestamp cursor
// silently dropped rows on page boundaries that fell on a shared
// received_at = NOW() value (which happens whenever a client uploads
// a batch in a single transaction).
func TestListEvents_PaginatesAcrossSharedReceivedAt(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	user, err := s.UpsertUser(ctx, "apple-sub-pagination", "p@example.com", false)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Insert 250 events through the store API. The store wraps every
	// batch in a single transaction with NOW(), so all 250 rows share
	// the same received_at — exactly the condition the bug needs.
	const total = 250
	batch := make([]store.SnoreEvent, total)
	for i := range batch {
		batch[i] = store.SnoreEvent{
			ClientEventID: fmt.Sprintf("ev-%03d", i),
			StartedAt:     time.Now().UTC(),
			DurationMS:    1000,
			AvgDB:         60.0,
		}
	}
	n, err := s.InsertEvents(ctx, user.ID, batch)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if n != total {
		t.Fatalf("inserted %d, want %d", n, total)
	}

	const pageSize = 100
	seen := map[string]bool{}
	cursor := store.ZeroCursor()
	for page := 0; page < (total/pageSize)+5; page++ {
		rows, err := s.ListEvents(ctx, user.ID, cursor, pageSize)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, e := range rows {
			if seen[e.ClientEventID] {
				t.Fatalf("duplicate %s on page %d", e.ClientEventID, page)
			}
			seen[e.ClientEventID] = true
			cursor = store.Cursor{ReceivedAt: e.ReceivedAt, ID: e.ID}
		}
	}
	if len(seen) != total {
		t.Errorf("paginated %d rows, want %d (data loss in cursor pagination)", len(seen), total)
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

// --- Phase B: recording sessions + audio clips ---

func TestUpsertSession_Idempotent(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	user, _ := s.UpsertUser(ctx, "apple-sub-sess", "u@x.com", false)
	first, created, err := s.UpsertSession(ctx, user.ID, store.RecordingSession{
		ClientSessionID: "csid-1",
		StartedAt:       time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC),
		DeviceName:      "iPhone 16",
		AppVersion:      "1.0.0",
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if !created {
		t.Errorf("first upsert created=false")
	}
	// Second upsert with same client_session_id + a new ended_at must
	// return the *same* row id and update the mutable fields.
	end := time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)
	second, created2, err := s.UpsertSession(ctx, user.ID, store.RecordingSession{
		ClientSessionID: "csid-1",
		StartedAt:       time.Date(2026, 5, 5, 1, 0, 0, 0, time.UTC),
		EndedAt:         &end,
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if created2 {
		t.Errorf("second upsert created=true; idempotency broken")
	}
	if second.ID != first.ID {
		t.Errorf("session id changed: %s -> %s", first.ID, second.ID)
	}
	if second.EndedAt == nil || !second.EndedAt.Equal(end) {
		t.Errorf("ended_at not propagated: %v", second.EndedAt)
	}
	if second.DeviceName != "iPhone 16" {
		t.Errorf("device_name overwritten: %q", second.DeviceName)
	}
}

// TestListSessions_DescOrderingWithSharedStartedAt is the parallel of
// TestListEvents_PaginatesAcrossSharedReceivedAt — confirms that the
// DESC compound cursor pages cleanly when many sessions share a
// started_at to the nanosecond.
func TestListSessions_DescOrderingWithSharedStartedAt(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	user, _ := s.UpsertUser(ctx, "apple-sub-sess-page", "u@x.com", false)
	sharedAt := time.Date(2026, 5, 5, 4, 0, 0, 0, time.UTC)
	const total = 25
	for i := 0; i < total; i++ {
		_, _, err := s.UpsertSession(ctx, user.ID, store.RecordingSession{
			ClientSessionID: fmt.Sprintf("page-sess-%03d", i),
			StartedAt:       sharedAt,
		})
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	const pageSize = 7
	seen := map[string]bool{}
	cursor := store.DescCursor{}
	for page := 0; page < (total/pageSize)+5; page++ {
		rows, err := s.ListSessions(ctx, user.ID, cursor, pageSize)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			if seen[r.ClientSessionID] {
				t.Fatalf("duplicate %s on page %d", r.ClientSessionID, page)
			}
			seen[r.ClientSessionID] = true
			cursor = store.DescCursor{StartedAt: r.StartedAt, ID: r.ID}
		}
	}
	if len(seen) != total {
		t.Errorf("paginated %d, want %d (data loss in DESC cursor)", len(seen), total)
	}
}

func TestUpsertAudioClip_Idempotent(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	user, _ := s.UpsertUser(ctx, "apple-sub-clip", "u@x.com", false)
	in := store.AudioClip{
		ClientClipID: "clip-A",
		StartedAt:    time.Now().UTC(),
		DurationMS:   2000,
		ContentType:  "audio/m4a",
		SizeBytes:    1024,
		SHA256:       fmt.Sprintf("%064d", 1),
		ObjectKey:    "clips/" + user.ID.String() + "/" + uuid.NewString(),
	}
	first, created, err := s.UpsertAudioClip(ctx, user.ID, in)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if !created {
		t.Errorf("first upsert created=false")
	}
	// Re-insert with a *different* object_key. Idempotency means we
	// return the existing row + do NOT overwrite the stored key.
	in2 := in
	in2.ObjectKey = "clips/" + user.ID.String() + "/" + uuid.NewString()
	second, created2, err := s.UpsertAudioClip(ctx, user.ID, in2)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if created2 {
		t.Errorf("second upsert created=true; idempotency broken")
	}
	if second.ID != first.ID {
		t.Errorf("clip id changed: %s -> %s", first.ID, second.ID)
	}
	if second.ObjectKey != first.ObjectKey {
		t.Errorf("object_key overwritten: %q -> %q", first.ObjectKey, second.ObjectKey)
	}
}

func TestSoftDeleteAudioClip_HidesFromList(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	user, _ := s.UpsertUser(ctx, "apple-sub-clip-del", "u@x.com", false)
	row, _, err := s.UpsertAudioClip(ctx, user.ID, store.AudioClip{
		ClientClipID: "clip-DEL",
		StartedAt:    time.Now().UTC(),
		DurationMS:   1500,
		ContentType:  "audio/m4a",
		SizeBytes:    1024,
		SHA256:       fmt.Sprintf("%064d", 2),
		ObjectKey:    "clips/" + user.ID.String() + "/" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.SoftDeleteAudioClip(ctx, user.ID, row.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// List skips it.
	got, err := s.ListAudioClips(ctx, user.ID, store.DescCursor{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("deleted clip still listed: %+v", got)
	}
	// Get returns ErrNotFound.
	if _, err := s.GetAudioClip(ctx, user.ID, row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get returned %v, want ErrNotFound", err)
	}
	// Second delete is also ErrNotFound.
	if err := s.SoftDeleteAudioClip(ctx, user.ID, row.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete returned %v, want ErrNotFound", err)
	}
}

func TestAudioClips_CrossUserIsolation(t *testing.T) {
	s, done := freshDB(t)
	defer done()
	ctx := context.Background()

	a, _ := s.UpsertUser(ctx, "apple-sub-iso-a", "a@x.com", false)
	b, _ := s.UpsertUser(ctx, "apple-sub-iso-b", "b@x.com", false)
	clip, _, err := s.UpsertAudioClip(ctx, a.ID, store.AudioClip{
		ClientClipID: "iso-clip",
		StartedAt:    time.Now().UTC(),
		DurationMS:   1500,
		ContentType:  "audio/m4a",
		SizeBytes:    100,
		SHA256:       fmt.Sprintf("%064d", 3),
		ObjectKey:    "clips/" + a.ID.String() + "/" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	// b should not see a's clip via List or Get.
	rows, err := s.ListAudioClips(ctx, b.ID, store.DescCursor{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("b sees %d of a's clips: %+v", len(rows), rows)
	}
	if _, err := s.GetAudioClip(ctx, b.ID, clip.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("b can read a's clip via Get: %v", err)
	}
	// b's soft-delete on a's id is a no-op (ErrNotFound).
	if err := s.SoftDeleteAudioClip(ctx, b.ID, clip.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("b can soft-delete a's clip: %v", err)
	}
	// Confirm a's clip is still there.
	if _, err := s.GetAudioClip(ctx, a.ID, clip.ID); err != nil {
		t.Errorf("a's clip wrongly mutated by b's delete attempt: %v", err)
	}
}