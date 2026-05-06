package blobstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
)

// fakeGCSClient is an in-memory gcsClient backing for tests. It
// records the keys passed in (after the prefix is applied), stores
// the bytes + content-type Put writes, and returns those bytes on
// Get. Tracks the underlying object map under a mutex so the
// race-detector smoke test in blobstore_test.go is also safe with
// the GCS variant.
type fakeGCSClient struct {
	mu               sync.Mutex
	puts             map[string][]byte
	contentTypes     map[string]string
	errOnPutClose    error
	simulateNotFound bool
}

func newFakeGCSClient() *fakeGCSClient {
	return &fakeGCSClient{
		puts:         map[string][]byte{},
		contentTypes: map[string]string{},
	}
}

func (f *fakeGCSClient) Bucket(_ string) gcsBucket { return &fakeGCSBucket{c: f} }
func (f *fakeGCSClient) Close() error              { return nil }

type fakeGCSBucket struct{ c *fakeGCSClient }

func (b *fakeGCSBucket) Object(name string) gcsObject {
	return &fakeGCSObject{c: b.c, name: name}
}

type fakeGCSObject struct {
	c    *fakeGCSClient
	name string
}

func (o *fakeGCSObject) NewWriter(_ context.Context) gcsWriter {
	return &fakeGCSWriter{c: o.c, name: o.name, buf: &bytes.Buffer{}}
}

func (o *fakeGCSObject) NewReader(_ context.Context) (gcsReader, error) {
	o.c.mu.Lock()
	defer o.c.mu.Unlock()
	if o.c.simulateNotFound {
		return nil, storage.ErrObjectNotExist
	}
	body, ok := o.c.puts[o.name]
	if !ok {
		return nil, storage.ErrObjectNotExist
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	return &fakeGCSReader{
		Reader:      bytes.NewReader(cp),
		contentType: o.c.contentTypes[o.name],
		size:        int64(len(cp)),
	}, nil
}

func (o *fakeGCSObject) Delete(_ context.Context) error {
	o.c.mu.Lock()
	defer o.c.mu.Unlock()
	if o.c.simulateNotFound {
		return storage.ErrObjectNotExist
	}
	if _, ok := o.c.puts[o.name]; !ok {
		return storage.ErrObjectNotExist
	}
	delete(o.c.puts, o.name)
	delete(o.c.contentTypes, o.name)
	return nil
}

type fakeGCSWriter struct {
	c           *fakeGCSClient
	name        string
	contentType string
	buf         *bytes.Buffer
	closed      bool
}

func (w *fakeGCSWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *fakeGCSWriter) Close() error {
	if w.closed {
		return errors.New("fakeGCSWriter: already closed")
	}
	w.closed = true
	if w.c.errOnPutClose != nil {
		return w.c.errOnPutClose
	}
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	cp := make([]byte, w.buf.Len())
	copy(cp, w.buf.Bytes())
	w.c.puts[w.name] = cp
	w.c.contentTypes[w.name] = w.contentType
	return nil
}

func (w *fakeGCSWriter) SetContentType(ct string) { w.contentType = ct }

type fakeGCSReader struct {
	*bytes.Reader
	contentType string
	size        int64
}

func (r *fakeGCSReader) Close() error          { return nil }
func (r *fakeGCSReader) ContentType() string   { return r.contentType }
func (r *fakeGCSReader) Size() int64           { return r.size }

// --- tests ----------------------------------------------------------

func TestGCS_PutGetDelete_RoundTrip(t *testing.T) {
	fake := newFakeGCSClient()
	g := newGCSWithClient(fake, "test-bucket", "")
	ctx := context.Background()
	body := []byte("audio bytes round-trip")

	if err := g.Put(ctx, "u/abc", bytes.NewReader(body), "audio/m4a"); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, meta, err := g.Get(ctx, "u/abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
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

	if err := g.Delete(ctx, "u/abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := g.Get(ctx, "u/abc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestGCS_Get_NotFound_MapsToErrNotFound(t *testing.T) {
	fake := newFakeGCSClient()
	fake.simulateNotFound = true
	g := newGCSWithClient(fake, "test-bucket", "")

	_, _, err := g.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGCS_Delete_NotFound_MapsToErrNotFound(t *testing.T) {
	fake := newFakeGCSClient()
	fake.simulateNotFound = true
	g := newGCSWithClient(fake, "test-bucket", "")

	err := g.Delete(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGCS_PrefixIsAppliedToAllOps(t *testing.T) {
	fake := newFakeGCSClient()
	g := newGCSWithClient(fake, "test-bucket", "clips/")
	ctx := context.Background()

	if err := g.Put(ctx, "u/abc", bytes.NewReader([]byte("x")), "audio/m4a"); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Inspect the fake's storage directly: the stored key must be the
	// prefixed full key, not the bare logical key.
	fake.mu.Lock()
	keys := make([]string, 0, len(fake.puts))
	for k := range fake.puts {
		keys = append(keys, k)
	}
	fake.mu.Unlock()
	if len(keys) != 1 {
		t.Fatalf("expected 1 stored key, got %d (%v)", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], "clips/") {
		t.Errorf("stored key %q must start with prefix %q", keys[0], "clips/")
	}
	if keys[0] != "clips/u/abc" {
		t.Errorf("stored key = %q, want %q", keys[0], "clips/u/abc")
	}

	// Get must also use the prefixed key.
	rc, _, err := g.Get(ctx, "u/abc")
	if err != nil {
		t.Fatalf("get with prefix: %v", err)
	}
	_ = rc.Close()

	// Delete must also use the prefixed key.
	if err := g.Delete(ctx, "u/abc"); err != nil {
		t.Fatalf("delete with prefix: %v", err)
	}
	fake.mu.Lock()
	remaining := len(fake.puts)
	fake.mu.Unlock()
	if remaining != 0 {
		t.Errorf("after delete: %d objects remain, want 0", remaining)
	}
}

func TestGCS_PutPropagatesWriterError(t *testing.T) {
	fake := newFakeGCSClient()
	fake.errOnPutClose = errors.New("simulated close failure")
	g := newGCSWithClient(fake, "test-bucket", "")

	err := g.Put(context.Background(), "k", bytes.NewReader([]byte("x")), "audio/m4a")
	if err == nil {
		t.Fatal("expected put to fail when writer Close returns error")
	}
	if !strings.Contains(err.Error(), "simulated close failure") {
		t.Errorf("error %v does not wrap the underlying writer error", err)
	}
}

func TestGCS_NewGCS_RequiresBucket(t *testing.T) {
	// Pass a no-op context; if we tried to construct the real client
	// with empty creds we'd hit network. The bucket-is-empty check
	// must short-circuit before storage.NewClient is called.
	_, err := NewGCS(context.Background(), "", "clips/")
	if err == nil {
		t.Fatal("expected error for empty bucket")
	}
	if !strings.Contains(err.Error(), "bucket") {
		t.Errorf("error %q should mention bucket", err.Error())
	}
}

func TestGCS_NormalizePrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"clips", "clips/"},
		{"clips/", "clips/"},
		{"a/b", "a/b/"},
		{"a/b/", "a/b/"},
	}
	for _, tc := range cases {
		if got := NormalizePrefix(tc.in); got != tc.want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
