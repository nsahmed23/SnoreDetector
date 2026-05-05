package apple

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAudience = "com.snoreguard.app"
	testIssuer   = "https://appleid.apple.com"
)

// signedToken produces a token signed by `priv` with the given claims,
// returning the compact JWT string and the key id ("kid") header.
func signedToken(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// fixedNow is used so tests are deterministic vs. clock.
func fixedNow() time.Time {
	return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
}

func newTestVerifier(t *testing.T) (*Verifier, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	v := NewWithKeyfunc(testAudience, testIssuer, func(_ *jwt.Token) (any, error) {
		return &priv.PublicKey, nil
	})
	v.now = fixedNow
	return v, priv
}

func validClaims() jwt.MapClaims {
	now := fixedNow()
	return jwt.MapClaims{
		"iss":              testIssuer,
		"aud":              testAudience,
		"sub":              "001234.fakeappleuser.0001",
		"iat":              now.Add(-1 * time.Minute).Unix(),
		"exp":              now.Add(10 * time.Minute).Unix(),
		"email":            "user@privaterelay.appleid.com",
		"email_verified":   "true",
		"is_private_email": "true",
	}
}

func TestVerify_Happy(t *testing.T) {
	v, priv := newTestVerifier(t)
	tok := signedToken(t, priv, "k1", validClaims())

	id, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Subject != "001234.fakeappleuser.0001" {
		t.Errorf("Subject = %q", id.Subject)
	}
	if id.Email != "user@privaterelay.appleid.com" {
		t.Errorf("Email = %q", id.Email)
	}
	if !id.EmailVerified {
		t.Errorf("EmailVerified = false, want true")
	}
	if !id.IsPrivateEmail {
		t.Errorf("IsPrivateEmail = false, want true")
	}
}

func TestVerify_BoolBooleansAlsoAccepted(t *testing.T) {
	v, priv := newTestVerifier(t)
	c := validClaims()
	c["email_verified"] = true
	c["is_private_email"] = false
	tok := signedToken(t, priv, "k1", c)

	id, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !id.EmailVerified {
		t.Errorf("EmailVerified bool not accepted")
	}
	if id.IsPrivateEmail {
		t.Errorf("IsPrivateEmail should be false")
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	v, priv := newTestVerifier(t)
	c := validClaims()
	c["exp"] = fixedNow().Add(-1 * time.Minute).Unix()
	tok := signedToken(t, priv, "k1", c)

	_, err := v.Verify(tok)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestVerify_RejectsWrongAudience(t *testing.T) {
	v, priv := newTestVerifier(t)
	c := validClaims()
	c["aud"] = "com.someoneelse.app"
	tok := signedToken(t, priv, "k1", c)

	_, err := v.Verify(tok)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestVerify_RejectsWrongIssuer(t *testing.T) {
	v, priv := newTestVerifier(t)
	c := validClaims()
	c["iss"] = "https://accounts.google.com"
	tok := signedToken(t, priv, "k1", c)

	_, err := v.Verify(tok)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestVerify_RejectsMissingSub(t *testing.T) {
	v, priv := newTestVerifier(t)
	c := validClaims()
	delete(c, "sub")
	tok := signedToken(t, priv, "k1", c)

	_, err := v.Verify(tok)
	if err == nil || !strings.Contains(err.Error(), "sub") {
		t.Fatalf("expected error mentioning sub, got %v", err)
	}
}

func TestVerify_RejectsTamperedSignature(t *testing.T) {
	v, priv := newTestVerifier(t)
	tok := signedToken(t, priv, "k1", validClaims())
	// Flip a byte in the signature segment.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(parts))
	}
	parts[2] = parts[2][:len(parts[2])-2] + "AB"
	tampered := strings.Join(parts, ".")

	_, err := v.Verify(tampered)
	if err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestVerify_RejectsEmpty(t *testing.T) {
	v, _ := newTestVerifier(t)
	if _, err := v.Verify(""); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestVerify_RejectsHS256(t *testing.T) {
	// Apple signs with RS256/ES256. A forged HS256 token using the
	// public key as the secret must be rejected by the alg whitelist.
	v, priv := newTestVerifier(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
	tok.Header["kid"] = "k1"
	pubBytes, err := tok.SignedString([]byte("attacker-known-secret"))
	if err != nil {
		t.Fatalf("sign hs256: %v", err)
	}
	_ = priv

	_, err = v.Verify(pubBytes)
	if err == nil {
		t.Fatal("expected algorithm-confusion attack to be rejected")
	}
}
