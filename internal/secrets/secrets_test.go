package secrets

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newKEK(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil {
		t.Error("New(16 bytes) expected error, got nil")
	}
	if _, err := New(make([]byte, 32)); err != nil {
		t.Errorf("New(32 bytes) unexpected error: %v", err)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := New(newKEK(t))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("postgres://user:s3cr3t@db:5432/app")

	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip = %q, want %q", got, plaintext)
	}
}

func TestSealedCiphertextDoesNotContainPlaintext(t *testing.T) {
	c, _ := New(newKEK(t))
	plaintext := []byte("super-secret-value")

	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed.Ciphertext, plaintext) {
		t.Error("ciphertext leaks plaintext")
	}
	if len(sealed.EncryptedDEK) == 0 {
		t.Error("EncryptedDEK should be populated (envelope encryption)")
	}
}

func TestSamePlaintextProducesDifferentCiphertext(t *testing.T) {
	c, _ := New(newKEK(t))
	plaintext := []byte("repeat me")

	a, _ := c.Encrypt(plaintext)
	b, _ := c.Encrypt(plaintext)

	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Error("two encryptions produced identical ciphertext (nonce/DEK reuse?)")
	}
}

func TestTamperedCiphertextFailsAuthentication(t *testing.T) {
	c, _ := New(newKEK(t))
	sealed, _ := c.Encrypt([]byte("integrity matters"))

	sealed.Ciphertext[0] ^= 0xFF // flip a byte

	if _, err := c.Decrypt(sealed); err == nil {
		t.Error("Decrypt of tampered ciphertext should fail")
	}
}

func TestWrongKEKCannotDecrypt(t *testing.T) {
	c1, _ := New(newKEK(t))
	c2, _ := New(newKEK(t))

	sealed, _ := c1.Encrypt([]byte("for my eyes only"))

	if _, err := c2.Decrypt(sealed); err == nil {
		t.Error("Decrypt with a different KEK should fail")
	}
}

func TestKeyVersionIsRecorded(t *testing.T) {
	c, _ := New(newKEK(t))
	sealed, _ := c.Encrypt([]byte("x"))
	if sealed.KeyVersion != CurrentKeyVersion {
		t.Errorf("KeyVersion = %d, want %d", sealed.KeyVersion, CurrentKeyVersion)
	}
}
