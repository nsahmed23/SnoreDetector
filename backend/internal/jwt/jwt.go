// Package jwt issues + verifies the app's own session tokens.
//
// These tokens are issued *after* a successful Apple Sign-In and are
// what the iOS client uses on every subsequent request via the
// `Authorization: Bearer <token>` header. Apple identity tokens are
// short-lived and one-shot; our session token is what gives the client
// long-lived authenticated access.
package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Issuer struct {
	signingKey []byte
	issuer     string
	ttl        time.Duration
	now        func() time.Time
}

// Claims is the subset we put in our session tokens.
type Claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

// TTL returns the issuer's configured token lifetime.
func (i *Issuer) TTL() time.Duration { return i.ttl }

func New(signingKey []byte, issuer string, ttl time.Duration) (*Issuer, error) {
	if len(signingKey) < 32 {
		return nil, fmt.Errorf("jwt: signing key must be ≥ 32 bytes, got %d", len(signingKey))
	}
	if issuer == "" {
		return nil, errors.New("jwt: issuer is required")
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Issuer{signingKey: signingKey, issuer: issuer, ttl: ttl, now: time.Now}, nil
}

// Issue returns a signed session token for the given user.
func (i *Issuer) Issue(userID uuid.UUID) (string, error) {
	now := i.now()
	claims := Claims{
		UserID: userID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
			ID:        uuid.NewString(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(i.signingKey)
}

// Verify validates a session token and returns the user ID it was issued for.
func (i *Issuer) Verify(raw string) (uuid.UUID, error) {
	parser := jwt.NewParser(
		jwt.WithIssuer(i.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithTimeFunc(i.now),
	)

	tok, err := parser.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		return i.signingKey, nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("jwt: verify: %w", err)
	}
	c, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return uuid.Nil, errors.New("jwt: invalid claims")
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("jwt: bad uid: %w", err)
	}
	return id, nil
}
