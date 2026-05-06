// GCS-backed implementation of the Store interface.
//
// SECURITY: this adapter never issues signed URLs. Downloads stream
// through the API per ADR 0008 — the client always talks to our
// backend, our backend talks to GCS, and the bytes are proxied. That
// is what keeps object access subject to our authn + per-user
// authorization checks rather than a time-bounded URL that, once
// leaked, exposes the bytes outside the IAM perimeter.
//
// Object keys are server-controlled and prefixed by `BLOBSTORE_GCS_PREFIX`
// (default "clips/"). The user_id-scoping continues to live in the
// caller (cmd/sync-service constructs keys like
// `clips/<user_id>/<uuid>`); this package never touches user input.
//
// Test seam: production code calls *storage.Client; tests inject a
// fake via newGCSWithClient. The minimal gcsClient/gcsBucket/gcsObject
// surface is the boundary — only the operations we actually use
// (Put/Get/Delete + a NewReader/NewWriter pair + Delete) need to be
// modelled. Constructing the real *storage.Client is deferred to
// NewGCS so unit tests don't need credentials or network.
package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// gcsClient is the minimal subset of *storage.Client we need. Defined
// as an interface so unit tests can inject a fake without spinning up
// the real Google client (which probes credentials at construction).
type gcsClient interface {
	Bucket(name string) gcsBucket
	Close() error
}

type gcsBucket interface {
	Object(name string) gcsObject
}

type gcsObject interface {
	NewWriter(ctx context.Context) gcsWriter
	NewReader(ctx context.Context) (gcsReader, error)
	Delete(ctx context.Context) error
}

// gcsWriter abstracts *storage.Writer. The real type uses an embedded
// ObjectAttrs.ContentType field (not a setter); SetContentType is a
// thin shim around that field so the test fake can capture the value.
type gcsWriter interface {
	io.WriteCloser
	SetContentType(ct string)
}

// gcsReader abstracts *storage.Reader. The real type exposes
// ContentType() and Size() methods (rather than returning a separate
// *ObjectAttrs from NewReader); we mirror the same shape so the
// adapter has zero translation cost and the fake can supply both.
type gcsReader interface {
	io.ReadCloser
	ContentType() string
	Size() int64
}

// realGCSClient adapts *storage.Client to the gcsClient interface.
// Each adapter type below is a one-liner; the indirection exists
// solely so the test fake can stand in.
type realGCSClient struct{ c *storage.Client }

func (r realGCSClient) Bucket(name string) gcsBucket { return realGCSBucket{b: r.c.Bucket(name)} }
func (r realGCSClient) Close() error                 { return r.c.Close() }

type realGCSBucket struct{ b *storage.BucketHandle }

func (r realGCSBucket) Object(name string) gcsObject { return realGCSObject{o: r.b.Object(name)} }

type realGCSObject struct{ o *storage.ObjectHandle }

func (r realGCSObject) NewWriter(ctx context.Context) gcsWriter {
	return &realGCSWriter{w: r.o.NewWriter(ctx)}
}

func (r realGCSObject) NewReader(ctx context.Context) (gcsReader, error) {
	rd, err := r.o.NewReader(ctx)
	if err != nil {
		return nil, err
	}
	return realGCSReader{r: rd}, nil
}

func (r realGCSObject) Delete(ctx context.Context) error { return r.o.Delete(ctx) }

// realGCSWriter wraps *storage.Writer. The SDK exposes ContentType
// as a struct field on the embedded ObjectAttrs; SetContentType
// writes it for us so the fake can record the same call.
type realGCSWriter struct{ w *storage.Writer }

func (r *realGCSWriter) Write(p []byte) (int, error) { return r.w.Write(p) }
func (r *realGCSWriter) Close() error                { return r.w.Close() }
func (r *realGCSWriter) SetContentType(ct string)    { r.w.ContentType = ct }

type realGCSReader struct{ r *storage.Reader }

func (r realGCSReader) Read(p []byte) (int, error) { return r.r.Read(p) }
func (r realGCSReader) Close() error               { return r.r.Close() }
func (r realGCSReader) ContentType() string        { return r.r.Attrs.ContentType }
func (r realGCSReader) Size() int64                { return r.r.Attrs.Size }

// GCS implements blobstore.Store on top of Google Cloud Storage.
// Object keys are server-controlled and prefixed by `prefix`
// (typically "clips/"); the user_id-scoping continues to live in the
// caller.
type GCS struct {
	client gcsClient
	bucket string
	prefix string
}

// NewGCS builds a production GCS-backed Store. ctx controls only the
// initial client construction; subsequent calls take their own ctx.
// Application Default Credentials are honored by default; for local
// dev set GOOGLE_APPLICATION_CREDENTIALS to a service-account JSON
// path. Pass option.ClientOption values for tests / staging overrides.
func NewGCS(ctx context.Context, bucket, prefix string, opts ...option.ClientOption) (*GCS, error) {
	if bucket == "" {
		return nil, errors.New("blobstore: GCS bucket is required")
	}
	raw, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blobstore: gcs new client: %w", err)
	}
	return &GCS{
		client: realGCSClient{c: raw},
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// newGCSWithClient is the test-only constructor that takes a
// pre-built (likely fake) gcsClient. Production code uses NewGCS.
func newGCSWithClient(client gcsClient, bucket, prefix string) *GCS {
	return &GCS{client: client, bucket: bucket, prefix: prefix}
}

// Put streams r to the GCS object at `<prefix><key>`. The writer's
// ContentType is set before the first Write so the metadata is
// committed atomically with the body. Close() is what triggers the
// final commit; if Close errors after a successful Copy, the partial
// upload is aborted by the SDK and we surface a wrapped error.
func (g *GCS) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	fullKey := g.prefix + key
	w := g.client.Bucket(g.bucket).Object(fullKey).NewWriter(ctx)
	w.SetContentType(contentType)
	if _, err := io.Copy(w, r); err != nil {
		// Best-effort close on the failed writer to release the
		// resumable-upload session; ignore its error since we already
		// have a more meaningful one from Copy.
		_ = w.Close()
		return fmt.Errorf("blobstore: gcs put: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("blobstore: gcs put close: %w", err)
	}
	return nil
}

// Get opens a reader for `<prefix><key>`. storage.ErrObjectNotExist
// is normalised to blobstore.ErrNotFound so callers can keep using
// errors.Is the same way regardless of backend.
func (g *GCS) Get(ctx context.Context, key string) (io.ReadCloser, *Metadata, error) {
	fullKey := g.prefix + key
	obj := g.client.Bucket(g.bucket).Object(fullKey)
	rc, err := obj.NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("blobstore: gcs get: %w", err)
	}
	md := &Metadata{ContentType: rc.ContentType(), SizeBytes: rc.Size()}
	return rc, md, nil
}

// Delete removes the object at `<prefix><key>`. As with Get,
// storage.ErrObjectNotExist is normalised to blobstore.ErrNotFound.
func (g *GCS) Delete(ctx context.Context, key string) error {
	fullKey := g.prefix + key
	err := g.client.Bucket(g.bucket).Object(fullKey).Delete(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("blobstore: gcs delete: %w", err)
	}
	return nil
}

// Close releases the underlying client. Idempotent. Useful for
// graceful shutdown wiring; sync-service does not currently call it
// (the process exits and the connection pool is GC'd), but exposing
// it keeps the door open for cleaner shutdowns later.
func (g *GCS) Close() error {
	if g.client == nil {
		return nil
	}
	return g.client.Close()
}

// NormalizePrefix ensures a trailing slash on a non-empty prefix so
// concatenation with the key produces a clean path. Empty prefix
// passes through unchanged (callers may want unprefixed keys).
func NormalizePrefix(p string) string {
	if p == "" || strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}
