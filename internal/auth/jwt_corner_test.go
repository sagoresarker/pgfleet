package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testSecret is a 32-byte signing secret meeting MinSecretLen for tests.
var testSecret = []byte("0123456789abcdef0123456789abcdef")

// mustIssuer builds an Issuer or fails the test.
func mustIssuer(t *testing.T, secret []byte, ttl time.Duration, opts ...IssuerOption) *Issuer {
	t.Helper()
	i, err := NewIssuer(secret, ttl, opts...)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return i
}

// TestVerifyRejectsTokenWithoutExp — a correctly-signed token that carries no
// expiry must be rejected. Otherwise a leaked/forged token never expires,
// defeating TTL and rotation.
func TestVerifyRejectsTokenWithoutExp(t *testing.T) {
	secret := testSecret
	iss := mustIssuer(t, secret, time.Hour)

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: "u", Role: RoleAdmin, // no RegisteredClaims => no exp
	})
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := iss.Verify(signed); err == nil {
		t.Error("token without exp must be rejected")
	}
}

// TestVerifyRejectsUnknownRole — a correctly-signed token whose role claim is
// not a known role must be rejected at the trust boundary, not silently
// trusted (defense-in-depth against future allow-by-default RBAC changes).
func TestVerifyRejectsUnknownRole(t *testing.T) {
	secret := testSecret
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	iss := mustIssuer(t, secret, time.Hour, WithClock(func() time.Time { return now }))

	for _, role := range []Role{Role("superduper"), Role(""), Role("ADMIN")} {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
			UserID: "u", Role: role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			},
		})
		signed, _ := tok.SignedString(secret)
		if _, err := iss.Verify(signed); err == nil {
			t.Errorf("token with invalid role %q must be rejected", role)
		}
	}
}

// TestNewIssuerRejectsShortSecret — an Issuer must refuse a signing secret
// shorter than 32 bytes; an empty/short HMAC key makes tokens trivially
// forgeable. NewIssuer returns an error rather than producing a weak issuer.
func TestNewIssuerRejectsShortSecret(t *testing.T) {
	for _, secret := range [][]byte{nil, []byte(""), []byte("short"), make([]byte, 31)} {
		if _, err := NewIssuer(secret, time.Hour); err == nil {
			t.Errorf("NewIssuer must reject a %d-byte secret", len(secret))
		}
	}
	if _, err := NewIssuer(make([]byte, 32), time.Hour); err != nil {
		t.Errorf("NewIssuer must accept a 32-byte secret: %v", err)
	}
}

// TestVerifyRejectsFutureNotBefore — a token whose nbf is in the future is not
// yet valid and must be rejected.
func TestVerifyRejectsFutureNotBefore(t *testing.T) {
	secret := testSecret
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	iss := mustIssuer(t, secret, time.Hour, WithClock(func() time.Time { return now }))

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: "u", Role: RoleViewer,
		RegisteredClaims: jwt.RegisteredClaims{
			NotBefore: jwt.NewNumericDate(now.Add(time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Hour)),
		},
	})
	signed, _ := tok.SignedString(secret)
	if _, err := iss.Verify(signed); err == nil {
		t.Error("token with a future nbf must be rejected")
	}
}
