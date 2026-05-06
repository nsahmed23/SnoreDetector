package server

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nsahmed23/SnoreDetector/backend/internal/store"
)

// encodeCursor packs a (received_at, id) pair into an opaque base64url
// string. The format is `rfc3339nano + "|" + uuid`. RFC3339Nano never
// contains "|" so the separator is unambiguous.
func encodeCursor(c store.Cursor) string {
	raw := c.ReceivedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor inverts encodeCursor.
func decodeCursor(s string) (store.Cursor, error) {
	if s == "" {
		return store.Cursor{}, nil
	}
	t, id, err := decodeOpaque(s)
	if err != nil {
		return store.Cursor{}, err
	}
	return store.Cursor{ReceivedAt: t, ID: id}, nil
}

// encodeDescCursor packs a DESC-pagination (started_at, id) pair using
// the same base64-of-`rfc3339nano|uuid` format as encodeCursor. The
// shape is identical so clients don't need a second decoder; the
// difference is only how the server interprets it on read (less-than
// instead of greater-than).
func encodeDescCursor(c store.DescCursor) string {
	raw := c.StartedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeDescCursor inverts encodeDescCursor.
func decodeDescCursor(s string) (store.DescCursor, error) {
	if s == "" {
		return store.DescCursor{}, nil
	}
	t, id, err := decodeOpaque(s)
	if err != nil {
		return store.DescCursor{}, err
	}
	return store.DescCursor{StartedAt: t, ID: id}, nil
}

// decodeOpaque is the shared base64 → (timestamp, uuid) decoder used
// by both encodeCursor and encodeDescCursor. Lives here so any future
// format tweak is single-sourced.
func decodeOpaque(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Tolerate clients that accidentally include = padding.
		raw, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return time.Time{}, uuid.Nil, errors.New("invalid cursor encoding")
		}
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, errors.New("invalid cursor timestamp")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, errors.New("invalid cursor id")
	}
	return t.UTC(), id, nil
}
