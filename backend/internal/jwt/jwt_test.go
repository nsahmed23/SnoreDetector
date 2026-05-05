package jwt

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testKey = "test-signing-key-must-be-at-least-32-bytes-long!"

func TestNew_RejectsShortKey(t *testing.T) {
	_, err := New([]byte("too-short"), "iss", 0)
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestNew_RejectsEmptyIssuer(t *testing.T) {
	_, err := New([]byte(testKey), "", 0)
	if err == nil {
		t.Fatal("expected error for empty issuer")
	}
}

func TestIssueAndVerify_RoundTrip(t *testing.T) {
	iss, err := New([]byte(testKey), "snoreguard-test", time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	uid := uuid.New()
	tok, err := iss.Issue(uid)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := iss.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got != uid {
		t.Errorf("uid mismatch: got %s, want %s", got, uid)
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	iss, _ := New([]byte(testKey), "snoreguard-test", time.Hour)
	// Roll clock back when issuing, then forward when verifying.
	frozen := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	iss.now = func() time.Time { return frozen.Add(-2 * time.Hour) }
	tok, _ := iss.Issue(uuid.New())
	iss.now = func() time.Time { return frozen }

	if _, err := iss.Verify(tok); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestVerify_RejectsWrongIssuer(t *testing.T) {
	a, _ := New([]byte(testKey), "issuer-A", time.Hour)
	b, _ := New([]byte(testKey), "issuer-B", time.Hour)
	tok, _ := a.Issue(uuid.New())

	if _, err := b.Verify(tok); err == nil {
		t.Fatal("expected error when verifier issuer differs")
	}
}

func TestVerify_RejectsWrongKey(t *testing.T) {
	a, _ := New([]byte(testKey), "iss", time.Hour)
	other := strings.Repeat("x", 32)
	b, _ := New([]byte(other), "iss", time.Hour)

	tok, _ := a.Issue(uuid.New())
	if _, err := b.Verify(tok); err == nil {
		t.Fatal("expected error when signing key differs")
	}
}

func TestVerify_RejectsTampered(t *testing.T) {
	iss, _ := New([]byte(testKey), "iss", time.Hour)
	tok, _ := iss.Issue(uuid.New())
	parts := strings.Split(tok, ".")
	parts[1] = parts[1][:len(parts[1])-2] + "XX"
	if _, err := iss.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestVerify_RejectsNonHS256(t *testing.T) {
	// Crafted "none"-alg or RS-alg tokens must be rejected.
	iss, _ := New([]byte(testKey), "iss", time.Hour)
	// Header for alg=none + same body as a real token.
	tok, _ := iss.Issue(uuid.New())
	parts := strings.Split(tok, ".")
	// Replace header with one declaring alg=none.
	noneHeader := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0"
	forged := noneHeader + "." + parts[1] + "."
	if _, err := iss.Verify(forged); err == nil {
		t.Fatal("expected error for alg=none token")
	}
}
