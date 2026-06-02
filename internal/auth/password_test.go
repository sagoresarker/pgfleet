package auth

import (
	"strings"
	"testing"
)

func TestHashThenVerifySucceeds(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("verify of correct password should succeed")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hash, _ := HashPassword("s3cr3t")
	ok, err := VerifyPassword("not-the-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Error("verify of wrong password should fail")
	}
}

func TestHashIsPHCEncodedArgon2id(t *testing.T) {
	hash, err := HashPassword("anything")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash should be PHC argon2id, got %q", hash)
	}
}

func TestSamePasswordYieldsDifferentHashes(t *testing.T) {
	a, _ := HashPassword("repeat")
	b, _ := HashPassword("repeat")
	if a == b {
		t.Error("hashes of the same password should differ (random salt)")
	}
}

func TestVerifyRejectsMalformedEncodings(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=3,p=2$badsalt", // missing hash segment
		"$argon2id$v=19$bad-params$c2FsdA$aGFzaA",
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA", // wrong algorithm
	}
	for _, enc := range cases {
		t.Run(enc, func(t *testing.T) {
			ok, err := VerifyPassword("pw", enc)
			if ok {
				t.Error("malformed encoding should not verify as ok")
			}
			if err == nil {
				t.Error("malformed encoding should return an error")
			}
		})
	}
}

func TestVerifyIsRobustToEmptyPassword(t *testing.T) {
	hash, err := HashPassword("")
	if err != nil {
		t.Fatalf("HashPassword(\"\"): %v", err)
	}
	ok, err := VerifyPassword("", hash)
	if err != nil || !ok {
		t.Errorf("empty password should hash and verify, ok=%v err=%v", ok, err)
	}
	ok, _ = VerifyPassword("x", hash)
	if ok {
		t.Error("non-empty password must not verify against empty-password hash")
	}
}

func TestNeedsRehashDetectsWeakerParams(t *testing.T) {
	// A hash produced with non-current parameters should be flagged for rehash.
	weak := "$argon2id$v=19$m=8,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo"
	if !NeedsRehash(weak) {
		t.Error("a hash with weaker params should need rehashing")
	}

	current, _ := HashPassword("pw")
	if NeedsRehash(current) {
		t.Error("a freshly produced hash should not need rehashing")
	}
}
