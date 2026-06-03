package auth

import (
	"strings"
	"testing"
)

// TestVerifyPasswordEmptyHashSegmentDoesNotPanic guards a remote-DoS: an
// encoded hash whose final (hash) segment is empty decodes to keyLen==0, and
// argon2.IDKey with keyLen 0 panics with a nil-pointer deref inside blake2b.
// decodeHash must reject it as malformed instead.
func TestVerifyPasswordEmptyHashSegmentDoesNotPanic(t *testing.T) {
	enc := "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$" // empty hash segment
	ok, err := VerifyPassword("pw", enc)
	if ok {
		t.Error("empty-hash encoding must not verify")
	}
	if err == nil {
		t.Error("empty-hash encoding must return ErrInvalidHash, not panic")
	}
}

// TestVerifyPasswordEmptySaltSegmentRejected — an empty salt segment is an
// invalid argon2 hash and must be rejected, not silently accepted.
func TestVerifyPasswordEmptySaltSegmentRejected(t *testing.T) {
	enc := "$argon2id$v=19$m=19456,t=2,p=1$$aGFzaGhhc2hoYXNo" // empty salt
	ok, err := VerifyPassword("pw", enc)
	if ok {
		t.Error("empty-salt encoding must not verify")
	}
	if err == nil {
		t.Error("empty-salt encoding must return ErrInvalidHash")
	}
}

// TestDecodeHashRejectsZeroCostParams — m=0, t=0, or p=0 are not valid argon2
// parameters; Sscanf accepts the zeros, so decodeHash must reject them.
func TestDecodeHashRejectsZeroCostParams(t *testing.T) {
	cases := []string{
		"$argon2id$v=19$m=0,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo",
		"$argon2id$v=19$m=19456,t=0,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo",
		"$argon2id$v=19$m=19456,t=2,p=0$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo",
	}
	for _, enc := range cases {
		t.Run(enc, func(t *testing.T) {
			ok, err := VerifyPassword("pw", enc)
			if ok {
				t.Error("zero-cost params must not verify")
			}
			if err == nil {
				t.Error("zero-cost params must return ErrInvalidHash")
			}
		})
	}
}

// TestDecodeHashRejectsTrailingGarbage — fmt.Sscanf stops at the format and
// ignores trailing input, so non-canonical version/param strings were silently
// accepted. The canonical PHC parser must reject them.
func TestDecodeHashRejectsTrailingGarbage(t *testing.T) {
	cases := []string{
		"$argon2id$v=19garbage$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo",
		"$argon2id$v=19$m=19456,t=2,p=1,XYZ$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo",
		"$argon2id$v=19$m=19456,t=2,p=1extra$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo",
	}
	for _, enc := range cases {
		t.Run(enc, func(t *testing.T) {
			ok, err := VerifyPassword("pw", enc)
			if ok {
				t.Error("trailing-garbage encoding must not verify")
			}
			if err == nil {
				t.Error("trailing-garbage encoding must return ErrInvalidHash")
			}
		})
	}
}

// TestDecodeHashRejectsExtraSegments — a 7-field string (trailing '$') must be
// rejected.
func TestDecodeHashRejectsExtraSegments(t *testing.T) {
	enc := "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo$"
	ok, err := VerifyPassword("pw", enc)
	if ok || err == nil {
		t.Errorf("7-segment encoding must be rejected, ok=%v err=%v", ok, err)
	}
}

// TestHashVerifyAdversarialPasswords — passwords with NUL bytes, unicode, and
// very long inputs must round-trip, and a different password must not verify.
func TestHashVerifyAdversarialPasswords(t *testing.T) {
	cases := []string{
		"with\x00nul\x00bytes",
		"unicode 🔐 пароль パスワード",
		"   leading and trailing spaces   ",
		strings.Repeat("a", 1<<16), // 64 KiB
	}
	for i, pw := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			h, err := HashPassword(pw)
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			ok, err := VerifyPassword(pw, h)
			if err != nil || !ok {
				t.Fatalf("round-trip failed ok=%v err=%v", ok, err)
			}
			if ok, _ := VerifyPassword(pw+"x", h); ok {
				t.Error("a different password must not verify")
			}
		})
	}
}

// TestNeedsRehashShortKey — a hash whose decoded key is shorter than the
// current keyLen must be flagged for rehash.
func TestNeedsRehashShortKey(t *testing.T) {
	// 4-byte hash ("aGFz" decodes to 4 bytes) with otherwise-current params.
	short := "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFz"
	if !NeedsRehash(short) {
		t.Error("a hash with a short key length should need rehashing")
	}
}
