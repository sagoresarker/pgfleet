package secrets

import (
	"bytes"
	"slices"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := New(make([]byte, 32))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestDecryptEmptySealedReturnsErrorNotPanic — a zero-value Sealed (e.g. a
// corrupt/NULL DB row) has nil nonces; GCM.Open panics on a wrong-length
// nonce, so Decrypt must validate lengths and return an error instead.
func TestDecryptEmptySealedReturnsErrorNotPanic(t *testing.T) {
	c := newTestCipher(t)
	if _, err := c.Decrypt(Sealed{}); err == nil {
		t.Error("empty Sealed must return an error")
	}
}

// TestDecryptWrongNonceLengthReturnsErrorNotPanic — a truncated nonce must
// error, not panic.
func TestDecryptWrongNonceLengthReturnsErrorNotPanic(t *testing.T) {
	c := newTestCipher(t)
	s, _ := c.Encrypt([]byte("hello"))

	bad := s
	bad.DEKNonce = s.DEKNonce[:5]
	if _, err := c.Decrypt(bad); err == nil {
		t.Error("truncated DEKNonce must return an error")
	}

	bad = s
	bad.Nonce = slices.Clone(s.Nonce)
	bad.Nonce = bad.Nonce[:3]
	if _, err := c.Decrypt(bad); err == nil {
		t.Error("truncated Nonce must return an error")
	}
}

// TestDecryptTamperedFieldsFail — flipping a byte in ANY field must fail
// authentication.
func TestDecryptTamperedFieldsFail(t *testing.T) {
	c := newTestCipher(t)
	s, _ := c.Encrypt([]byte("top secret"))

	fields := map[string]func(Sealed) Sealed{
		"Ciphertext":   func(x Sealed) Sealed { x.Ciphertext = flip(x.Ciphertext); return x },
		"Nonce":        func(x Sealed) Sealed { x.Nonce = flip(x.Nonce); return x },
		"EncryptedDEK": func(x Sealed) Sealed { x.EncryptedDEK = flip(x.EncryptedDEK); return x },
		"DEKNonce":     func(x Sealed) Sealed { x.DEKNonce = flip(x.DEKNonce); return x },
	}
	for name, mutate := range fields {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Decrypt(mutate(clone(s))); err == nil {
				t.Errorf("tampering %s should fail decryption", name)
			}
		})
	}
}

// TestEncryptDecryptEmptyAndLarge — empty and large plaintexts round-trip.
func TestEncryptDecryptEmptyAndLarge(t *testing.T) {
	c := newTestCipher(t)
	for _, pt := range [][]byte{{}, nil, bytes.Repeat([]byte("x"), 1<<20)} {
		s, err := c.Encrypt(pt)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		got, err := c.Decrypt(s)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if len(got) != len(pt) {
			t.Errorf("round-trip len = %d, want %d", len(got), len(pt))
		}
	}
}

// TestNewKeyLengthBoundaries — only a 32-byte KEK is accepted.
func TestNewKeyLengthBoundaries(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("New with %d-byte key should fail", n)
		}
	}
	if _, err := New(make([]byte, 32)); err != nil {
		t.Errorf("New with 32-byte key should succeed: %v", err)
	}
}

// TestMarshalRoundTrip — Marshal/Unmarshal preserves a sealed secret and the
// result decrypts.
func TestMarshalRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	s, _ := c.Encrypt([]byte("payload"))
	data, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	back, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	pt, err := c.Decrypt(back)
	if err != nil || string(pt) != "payload" {
		t.Errorf("round-trip decrypt = %q err=%v", pt, err)
	}
}

// TestUnmarshalWrongShapeDoesNotDecrypt — valid JSON of the wrong shape yields
// a degenerate Sealed that fails to decrypt (and must not panic).
func TestUnmarshalWrongShapeDoesNotDecrypt(t *testing.T) {
	c := newTestCipher(t)
	back, err := Unmarshal([]byte(`{"v":5}`))
	if err != nil {
		t.Fatalf("Unmarshal of wrong-shape JSON should not error structurally: %v", err)
	}
	if _, err := c.Decrypt(back); err == nil {
		t.Error("wrong-shape Sealed must not decrypt")
	}
}

func clone(s Sealed) Sealed {
	return Sealed{
		Ciphertext:   slices.Clone(s.Ciphertext),
		Nonce:        slices.Clone(s.Nonce),
		EncryptedDEK: slices.Clone(s.EncryptedDEK),
		DEKNonce:     slices.Clone(s.DEKNonce),
		KeyVersion:   s.KeyVersion,
	}
}

func flip(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	out := slices.Clone(b)
	out[0] ^= 0x01
	return out
}
