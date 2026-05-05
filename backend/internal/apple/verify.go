// Package apple verifies Sign in with Apple identity tokens.
//
// The flow:
//  1. iOS issues `ASAuthorizationAppleIDRequest`, the user authenticates,
//     Apple returns a signed `identityToken` (a JWT) to the client.
//  2. The client posts that token to /auth/apple on this service.
//  3. We fetch (and cache) Apple's JWKS at https://appleid.apple.com/auth/keys
//     and verify: signature, `iss`, `aud`, `exp`, `iat`, optional `nonce`.
//
// The package is JWKS-source agnostic — tests inject a fake key set
// rather than hitting the network.
package apple

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

const AppleJWKSURL = "https://appleid.apple.com/auth/keys"

// Identity is the subset of claims we care about from a verified Apple token.
type Identity struct {
	// Subject is Apple's stable opaque user ID — primary key for the user row.
	Subject string
	// Email is only present on the user's first sign-in (and only if they
	// chose to share). Subsequent tokens omit it.
	Email string
	// EmailVerified mirrors Apple's `email_verified` claim.
	EmailVerified bool
	// IsPrivateEmail is true when Apple proxies the user's real address.
	IsPrivateEmail bool
}

// Verifier validates Apple identity tokens against a configured audience.
// Construct via [New] or [NewWithKeyfunc] (the latter for tests).
type Verifier struct {
	audience string
	issuer   string
	keyfunc  jwt.Keyfunc
	now      func() time.Time
}

// New constructs a verifier that fetches Apple's JWKS over HTTPS with
// in-memory caching + automatic refresh.
func New(ctx context.Context, audience, issuer string) (*Verifier, error) {
	if audience == "" {
		return nil, errors.New("apple: audience is required")
	}
	if issuer == "" {
		issuer = "https://appleid.apple.com"
	}
	k, err := keyfunc.NewDefaultCtx(ctx, []string{AppleJWKSURL})
	if err != nil {
		return nil, fmt.Errorf("apple: load JWKS: %w", err)
	}
	return &Verifier{
		audience: audience,
		issuer:   issuer,
		keyfunc:  k.Keyfunc,
		now:      time.Now,
	}, nil
}

// NewWithKeyfunc lets tests inject a fake JWKS.
func NewWithKeyfunc(audience, issuer string, kf jwt.Keyfunc) *Verifier {
	return &Verifier{
		audience: audience,
		issuer:   issuer,
		keyfunc:  kf,
		now:      time.Now,
	}
}

// SetClock overrides the time source used to validate `exp`/`iat`.
// Intended for tests.
func (v *Verifier) SetClock(now func() time.Time) {
	if now != nil {
		v.now = now
	}
}

// Verify parses + validates an Apple identity token and returns the
// extracted [Identity] on success.
func (v *Verifier) Verify(rawToken string) (*Identity, error) {
	if rawToken == "" {
		return nil, errors.New("apple: empty token")
	}

	parser := jwt.NewParser(
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithTimeFunc(v.now),
	)

	tok, err := parser.Parse(rawToken, v.keyfunc)
	if err != nil {
		return nil, fmt.Errorf("apple: verify: %w", err)
	}
	if !tok.Valid {
		return nil, errors.New("apple: token marked invalid")
	}

	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("apple: unexpected claims shape")
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, errors.New("apple: missing sub claim")
	}

	id := &Identity{Subject: sub}
	if e, ok := claims["email"].(string); ok {
		id.Email = e
	}
	if ev, ok := claims["email_verified"]; ok {
		id.EmailVerified = boolish(ev)
	}
	if pe, ok := claims["is_private_email"]; ok {
		id.IsPrivateEmail = boolish(pe)
	}
	return id, nil
}

// boolish accepts Apple's encoding of booleans, which sometimes ship
// as JSON booleans and sometimes as the strings "true" / "false".
func boolish(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	}
	return false
}
