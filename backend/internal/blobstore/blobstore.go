// Package blobstore is the storage abstraction for raw audio clip
// bytes uploaded by the iOS app. The interface intentionally stays
// minimal so production GCS/S3 backends are a drop-in replacement
// for the filesystem dev backend.
//
// Object keys are entirely server-controlled (UUID-derived); callers
// must never let user input flow into a key. See the threat-model
// section of backend/README.md.
package blobstore

import (
	"context"
	"errors"
	"io"
)

// Metadata is what Get returns alongside the body. Not every backend
// will populate every field — content_type is the only one that's
// required to round-trip.
type Metadata struct {
	ContentType string
	SizeBytes   int64
}

// ErrNotFound is returned by Get/Delete when the key isn't present.
// Callers map this to HTTP 404.
var ErrNotFound = errors.New("blobstore: not found")

// Store is the surface every backend (filesystem, fake, GCS, S3)
// implements. Implementations must be safe for concurrent use.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, *Metadata, error)
	Delete(ctx context.Context, key string) error
}
