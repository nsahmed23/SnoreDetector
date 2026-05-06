package blobstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Filesystem stores objects on the local filesystem under Root.
// Object content lives at `<Root>/<key>`; the (single) content-type
// header lives at a sidecar `<Root>/<key>.meta` so Get can reproduce
// it without sniffing.
//
// A per-store mutex serializes Put/Delete on the same key. Reads are
// not under the lock — once Put has finished writing, the file is
// immutable until the next Put/Delete, so a streaming Get can run
// concurrently with other Get calls for the same or different keys.
type Filesystem struct {
	Root string

	mu sync.Mutex // guards rename + sidecar write under a single key
}

// NewFilesystem returns a Filesystem rooted at `root`. The directory
// is created (with parents) on first use; callers don't have to
// pre-create it.
func NewFilesystem(root string) *Filesystem {
	return &Filesystem{Root: root}
}

func (f *Filesystem) path(key string) string { return filepath.Join(f.Root, key) }

// Put writes the object body at `<Root>/<key>` and its content-type
// sidecar at `<Root>/<key>.meta`. The body is written to a temp file
// in the same directory and atomically renamed into place, so a
// crash mid-Put can never leave a half-written object visible.
func (f *Filesystem) Put(_ context.Context, key string, r io.Reader, contentType string) error {
	if key == "" {
		return errors.New("blobstore: empty key")
	}
	dst := f.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("blobstore: mkdir: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp.*")
	if err != nil {
		return fmt.Errorf("blobstore: tempfile: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("blobstore: copy: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("blobstore: close tmp: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		cleanup()
		return fmt.Errorf("blobstore: rename: %w", err)
	}

	// Sidecar gets written *after* the body — if the process dies
	// between the two, Get falls back to application/octet-stream
	// rather than serving stale metadata.
	meta := dst + ".meta"
	if err := os.WriteFile(meta, []byte("Content-Type: "+contentType+"\n"), 0o644); err != nil {
		return fmt.Errorf("blobstore: write meta: %w", err)
	}
	return nil
}

// Get opens the object body for streaming and returns the cached
// content-type from the sidecar. Both file-not-found cases (missing
// body OR missing sidecar with body present) collapse to ErrNotFound.
func (f *Filesystem) Get(_ context.Context, key string) (io.ReadCloser, *Metadata, error) {
	if key == "" {
		return nil, nil, ErrNotFound
	}
	dst := f.path(key)
	st, err := os.Stat(dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("blobstore: stat: %w", err)
	}
	body, err := os.Open(dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("blobstore: open: %w", err)
	}
	ct, err := readContentType(dst + ".meta")
	if err != nil {
		_ = body.Close()
		return nil, nil, err
	}
	return body, &Metadata{ContentType: ct, SizeBytes: st.Size()}, nil
}

// Delete removes the body and its sidecar. Returns ErrNotFound if
// the body wasn't there to begin with (idempotency lives at the
// caller — a soft-delete already removes the row, and the object
// delete is best-effort).
func (f *Filesystem) Delete(_ context.Context, key string) error {
	if key == "" {
		return ErrNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	dst := f.path(key)
	if _, err := os.Stat(dst); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("blobstore: stat: %w", err)
	}
	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("blobstore: remove: %w", err)
	}
	// Sidecar may not exist if Put crashed between body and meta;
	// ignore "not found" on this branch.
	if err := os.Remove(dst + ".meta"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blobstore: remove meta: %w", err)
	}
	return nil
}

// readContentType reads the single-line sidecar `<key>.meta`. If the
// sidecar is missing OR malformed we fall back to a generic content
// type rather than failing the Get — the body still streams.
func readContentType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "application/octet-stream", nil
		}
		return "", fmt.Errorf("blobstore: open meta: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if v, ok := strings.CutPrefix(line, "Content-Type:"); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "application/octet-stream", nil
}
