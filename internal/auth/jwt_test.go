package auth

import (
	"strings"
	"testing"
	"time"
)

func TestIssueThenVerifyCarriesClaims(t *testing.T) {
	iss := NewIssuer([]byte("secret"), time.Hour)

	token, err := iss.Issue("user-1", "a@b.com", RoleOperator)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := iss.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UserID != "user-1" || claims.Email != "a@b.com" || claims.Role != RoleOperator {
		t.Errorf("claims = %+v", claims)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := base
	iss := NewIssuer([]byte("secret"), time.Hour, WithClock(func() time.Time { return clk }))

	token, _ := iss.Issue("u", "e", RoleViewer)

	clk = base.Add(2 * time.Hour) // advance past the 1h TTL
	if _, err := iss.Verify(token); err == nil {
		t.Error("expired token should fail verification")
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	iss := NewIssuer([]byte("secret"), time.Hour)
	token, _ := iss.Issue("u", "e", RoleAdmin)

	// Flip a character in the signature segment.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token shape: %q", token)
	}
	sig := []byte(parts[2])
	sig[0] ^= 0x01
	tampered := parts[0] + "." + parts[1] + "." + string(sig)

	if _, err := iss.Verify(tampered); err == nil {
		t.Error("tampered token should fail verification")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	issuer := NewIssuer([]byte("secret-A"), time.Hour)
	verifier := NewIssuer([]byte("secret-B"), time.Hour)

	token, _ := issuer.Issue("u", "e", RoleViewer)
	if _, err := verifier.Verify(token); err == nil {
		t.Error("token signed with a different secret should fail")
	}
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	iss := NewIssuer([]byte("secret"), time.Hour)
	// A token with alg "none" and no signature (classic JWT downgrade attack):
	// header {"alg":"none","typ":"JWT"} . body . (empty sig)
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJ1aWQiOiJ1IiwiZW1haWwiOiJlIiwicm9sZSI6ImFkbWluIn0."
	if _, err := iss.Verify(noneToken); err == nil {
		t.Error("alg=none token must be rejected")
	}
}

func TestVerifyRejectsMalformedToken(t *testing.T) {
	iss := NewIssuer([]byte("secret"), time.Hour)
	for _, bad := range []string{"", "not.a.jwt", "only-one-part", "a.b"} {
		if _, err := iss.Verify(bad); err == nil {
			t.Errorf("malformed token %q should fail", bad)
		}
	}
}
