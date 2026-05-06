package blobstore

import (
	"bytes"
	"context"
	"io"
	"sync"
)

// Fake is an in-memory Store. Useful for unit tests; the test-only
// Has and Bytes helpers let tests assert the bytes the handler put.
type Fake struct {
	mu          sync.Mutex
	bodies      map[string][]byte
	contentType map[string]string
}

// NewFake returns an empty in-memory blobstore.
func NewFake() *Fake {
	return &Fake{
		bodies:      map[string][]byte{},
		contentType: map[string]string{},
	}
}

func (f *Fake) Put(_ context.Context, key string, r io.Reader, contentType string) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Copy so callers can't mutate our backing storage.
	cp := make([]byte, len(body))
	copy(cp, body)
	f.bodies[key] = cp
	f.contentType[key] = contentType
	return nil
}

func (f *Fake) Get(_ context.Context, key string) (io.ReadCloser, *Metadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.bodies[key]
	if !ok {
		return nil, nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), &Metadata{
		ContentType: f.contentType[key],
		SizeBytes:   int64(len(body)),
	}, nil
}

func (f *Fake) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.bodies[key]; !ok {
		return ErrNotFound
	}
	delete(f.bodies, key)
	delete(f.contentType, key)
	return nil
}

// Has returns true iff the key is in the fake. Test-only.
func (f *Fake) Has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.bodies[key]
	return ok
}

// Bytes returns the body bytes for `key`, or nil if not present.
// Returns a copy so the caller can't mutate the backing slice. Test-only.
func (f *Fake) Bytes(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bodies[key]
	if !ok {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}
