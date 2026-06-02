package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Claims is the JWT payload for an authenticated session.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Role   Role   `json:"role"`
	jwt.RegisteredClaims
}

// Issuer mints and verifies HS256 session tokens.
type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// IssuerOption configures an Issuer.
type IssuerOption func(*Issuer)

// WithClock overrides the time source (used in tests for expiry).
func WithClock(now func() time.Time) IssuerOption {
	return func(i *Issuer) { i.now = now }
}

// NewIssuer builds an Issuer with the given signing secret and token TTL.
func NewIssuer(secret []byte, ttl time.Duration, opts ...IssuerOption) *Issuer {
	i := &Issuer{secret: secret, ttl: ttl, now: time.Now}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Issue mints a signed token for the given identity.
func (i *Issuer) Issue(userID, email string, role Role) (string, error) {
	now := i.now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a token, returning its claims. It rejects
// tokens not signed with HMAC (defending against the alg=none downgrade).
func (i *Issuer) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithTimeFunc(i.now),
	)
	_, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.KindUnauthorized, "auth: invalid token", err)
	}
	return claims, nil
}
