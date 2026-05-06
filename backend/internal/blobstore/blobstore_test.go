package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
)

// stores returns one constructor per impl so we can drive the same
// table tests across the filesystem and fake backends.
func stores(t *testing.T) []struct {
	name  string
	build func() Store
} {
	t.Helper()
	return []struct {
		name  string
		build func() Store
	}{
		{name: "filesystem", build: func() Store {
			return NewFilesystem(filepath.Join(t.TempDir(), "blobs"))
		}},
		{name: "fake", build: func() Store { return NewFake() }},
	}
}

func TestStore_PutGetRoundTrip(t *testing.T) {
	for _, tc := range stores(t) {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build()
			ctx := context.Background()
			body := []byte("hello, audio bytes")
			if err := s.Put(ctx, "u/abc", bytes.NewReader(body), "audio/m4a"); err != nil {
				t.Fatalf("put: %v", err)
			}
			rc, meta, err := s.Get(ctx, "u/abc")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer rc.Close()
			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if !bytes.Equal(got, body) {
				t.Errorf("body mismatch: got %q, want %q", got, body)
			}
			if meta.ContentType != "audio/m4a" {
				t.Errorf("content type = %q, want audio/m4a", meta.ContentType)
			}
			if meta.SizeBytes != int64(len(body)) {
				t.Errorf("size = %d, want %d", meta.SizeBytes, len(body))
			}
		})
	}
}

func TestStore_GetMissing(t *testing.T) {
	for _, tc := range stores(t) {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build()
			_, _, err := s.Get(context.Background(), "nope")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStore_DeleteMissing(t *testing.T) {
	for _, tc := range stores(t) {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build()
			err := s.Delete(context.Background(), "nope")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStore_PutDeleteGet(t *testing.T) {
	for _, tc := range stores(t) {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build()
			ctx := context.Background()
			if err := s.Put(ctx, "k", bytes.NewReader([]byte("x")), "audio/m4a"); err != nil {
				t.Fatalf("put: %v", err)
			}
			if err := s.Delete(ctx, "k"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			_, _, err := s.Get(ctx, "k")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("after delete, err = %v, want ErrNotFound", err)
			}
		})
	}
}

// TestStore_ConcurrentPutsAreSafe is the race-detector smoke test.
// Run via `go test -race ./internal/blobstore`. Concurrent Puts on
// different keys must complete without data races; concurrent Get
// on a previously-Put key must succeed.
func TestStore_ConcurrentPutsAreSafe(t *testing.T) {
	for _, tc := range stores(t) {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build()
			ctx := context.Background()
			var wg sync.WaitGroup
			for i := 0; i < 32; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					key := "k-" + string(rune('a'+i%26)) + "-" + string(rune('a'+(i/26)%26))
					body := bytes.Repeat([]byte{byte(i)}, 64)
					if err := s.Put(ctx, key, bytes.NewReader(body), "audio/m4a"); err != nil {
						t.Errorf("put: %v", err)
						return
					}
					rc, _, err := s.Get(ctx, key)
					if err != nil {
						t.Errorf("get: %v", err)
						return
					}
					_, _ = io.ReadAll(rc)
					_ = rc.Close()
				}(i)
			}
			wg.Wait()
		})
	}
}

func TestFake_HasAndBytes(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	if f.Has("k") {
		t.Error("empty fake should not Has k")
	}
	if err := f.Put(ctx, "k", bytes.NewReader([]byte("ab")), "audio/m4a"); err != nil {
		t.Fatal(err)
	}
	if !f.Has("k") {
		t.Error("fake.Has should be true after put")
	}
	got := f.Bytes("k")
	if !bytes.Equal(got, []byte("ab")) {
		t.Errorf("bytes = %q", got)
	}
	// Mutating the returned slice must not corrupt the backing slice.
	got[0] = 'z'
	if !bytes.Equal(f.Bytes("k"), []byte("ab")) {
		t.Error("fake exposed shared slice — caller mutation leaked")
	}
}
