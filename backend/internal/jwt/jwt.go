// Package jwt issues + verifies the app's own session tokens.
//
// These tokens are issued *after* a successful Apple Sign-In and are
// what the iOS client uses on every subsequent request via the
// `Authorization: Bearer <token>` header. Apple identity tokens are
// short-lived and one-shot; access tokens are short-lived (1h) and
// refresh tokens (60d) are persisted server-side so they can be
// rotated and revoked.
package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Default TTLs.
const (
	DefaultAccessTTL  = 1 * time.Hour
	DefaultRefreshTTL = 60 * 24 * time.Hour
)

type Issuer struct {
	signingKey     []byte
	issuer         string
	refreshIssuer  string
	ttl            time.Duration
	refreshTTL     time.Duration
	now            func() time.Time
}

// Claims is the subset we put in our access tokens.
type Claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

// RefreshClaims is the subset we put in our refresh tokens.
type RefreshClaims struct {
	UserID   string `json:"uid"`
	FamilyID string `json:"fid"`
	jwt.RegisteredClaims
}

// TTL returns the issuer's configured access-token lifetime.
func (i *Issuer) TTL() time.Duration { return i.ttl }

// RefreshTTL returns the issuer's configured refresh-token lifetime.
func (i *Issuer) RefreshTTL() time.Duration { return i.refreshTTL }

// New constructs an Issuer with the given signing key + issuer string.
// Pass `ttl <= 0` to fall back to DefaultAccessTTL.
func New(signingKey []byte, issuer string, ttl time.Duration) (*Issuer, error) {
	return NewWithRefresh(signingKey, issuer, "", ttl, 0)
}

// NewWithRefresh constructs an Issuer that can also mint refresh
// tokens. `refreshIssuer` is the `iss` claim used on refresh tokens
// (must differ from `issuer` so a refresh token can never be replayed
// as an access token). Pass empty `refreshIssuer` to default to
// `issuer + "-refresh"`. TTLs <= 0 fall back to defaults.
func NewWithRefresh(
	signingKey []byte,
	issuer, refreshIssuer string,
	accessTTL, refreshTTL time.Duration,
) (*Issuer, error) {
	if len(signingKey) < 32 {
		return nil, fmt.Errorf("jwt: signing key must be ≥ 32 bytes, got %d", len(signingKey))
	}
	if issuer == "" {
		return nil, errors.New("jwt: issuer is required")
	}
	if refreshIssuer == "" {
		refreshIssuer = issuer + "-refresh"
	}
	if refreshIssuer == issuer {
		return nil, errors.New("jwt: refresh issuer must differ from access issuer")
	}
	if accessTTL <= 0 {
		accessTTL = DefaultAccessTTL
	}
	if refreshTTL <= 0 {
		refreshTTL = DefaultRefreshTTL
	}
	return &Issuer{
		signingKey:    signingKey,
		issuer:        issuer,
		refreshIssuer: refreshIssuer,
		ttl:           accessTTL,
		refreshTTL:    refreshTTL,
		now:           time.Now,
	}, nil
}

// Issue returns a signed access token for the given user. The returned
// jti is the JWT ID claim — callers should keep it so they can later
// revoke the token via the revoked_jti table.
func (i *Issuer) Issue(userID uuid.UUID) (string, error) {
	tok, _, _, err := i.IssueAccess(userID)
	return tok, err
}

// IssueAccess returns a signed access token plus the JTI and expiry it
// was minted with.
func (i *Issuer) IssueAccess(userID uuid.UUID) (token string, jti uuid.UUID, expiresAt time.Time, err error) {
	now := i.now()
	jti = uuid.New()
	expiresAt = now.Add(i.ttl)
	claims := Claims{
		UserID: userID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        jti.String(),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(i.signingKey)
	if err != nil {
		return "", uuid.Nil, time.Time{}, err
	}
	return signed, jti, expiresAt, nil
}

// IssueRefresh returns a signed refresh token. The token's JTI doubles
// as its primary key in the refresh_tokens table.
func (i *Issuer) IssueRefresh(userID, familyID uuid.UUID) (token string, jti uuid.UUID, expiresAt time.Time, err error) {
	now := i.now()
	jti = uuid.New()
	expiresAt = now.Add(i.refreshTTL)
	claims := RefreshClaims{
		UserID:   userID.String(),
		FamilyID: familyID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.refreshIssuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        jti.String(),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(i.signingKey)
	if err != nil {
		return "", uuid.Nil, time.Time{}, err
	}
	return signed, jti, expiresAt, nil
}

// Verify validates an access token and returns the user ID it was
// issued for. Use VerifyAccess for the JTI as well.
func (i *Issuer) Verify(raw string) (uuid.UUID, error) {
	c, err := i.VerifyAccess(raw)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("jwt: bad uid: %w", err)
	}
	return id, nil
}

// VerifyAccess validates an access token and returns the parsed claims.
func (i *Issuer) VerifyAccess(raw string) (*Claims, error) {
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
		return nil, fmt.Errorf("jwt: verify: %w", err)
	}
	c, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("jwt: invalid claims")
	}
	return c, nil
}

// VerifyRefresh validates a refresh token and returns the parsed
// claims. Note: signature + TTL only — the caller must additionally
// consult refresh_tokens to check rotation/revocation status.
func (i *Issuer) VerifyRefresh(raw string) (*RefreshClaims, error) {
	parser := jwt.NewParser(
		jwt.WithIssuer(i.refreshIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithTimeFunc(i.now),
	)
	tok, err := parser.ParseWithClaims(raw, &RefreshClaims{}, func(t *jwt.Token) (any, error) {
		return i.signingKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwt: verify refresh: %w", err)
	}
	c, ok := tok.Claims.(*RefreshClaims)
	if !ok || !tok.Valid {
		return nil, errors.New("jwt: invalid refresh claims")
	}
	return c, nil
}
