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
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Tolerate clients that accidentally include = padding.
		raw, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return store.Cursor{}, errors.New("invalid cursor encoding")
		}
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return store.Cursor{}, errors.New("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return store.Cursor{}, errors.New("invalid cursor timestamp")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return store.Cursor{}, errors.New("invalid cursor id")
	}
	return store.Cursor{ReceivedAt: t.UTC(), ID: id}, nil
}
