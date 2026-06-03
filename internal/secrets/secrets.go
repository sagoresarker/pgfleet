// Package secrets implements envelope encryption for secrets at rest.
//
// Each secret is encrypted with a freshly generated 256-bit data-encryption
// key (DEK); the DEK is itself encrypted ("wrapped") with the long-lived
// key-encryption key (KEK) supplied at startup. Both layers use AES-256-GCM,
// which provides authenticated encryption (tamper detection). Storing the
// wrapped DEK alongside the ciphertext lets us rotate the KEK by re-wrapping
// DEKs without decrypting every secret.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// CurrentKeyVersion is stamped onto newly sealed secrets so the KEK can be
// rotated in the future.
const CurrentKeyVersion = 1

const keyLen = 32 // AES-256

// Sealed is the storable representation of an encrypted secret.
type Sealed struct {
	Ciphertext   []byte // secret encrypted under the DEK
	Nonce        []byte // GCM nonce for Ciphertext
	EncryptedDEK []byte // DEK encrypted (wrapped) under the KEK
	DEKNonce     []byte // GCM nonce for EncryptedDEK
	KeyVersion   int    // KEK version used to wrap the DEK
}

// Cipher encrypts and decrypts secrets using a KEK.
type Cipher struct {
	kekGCM cipher.AEAD
}

// New constructs a Cipher from a 32-byte KEK.
func New(kek []byte) (*Cipher, error) {
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, fmt.Errorf("secrets: invalid KEK: %w", err)
	}
	return &Cipher{kekGCM: gcm}, nil
}

// Encrypt seals plaintext using a fresh DEK wrapped under the KEK.
func (c *Cipher) Encrypt(plaintext []byte) (Sealed, error) {
	dek := make([]byte, keyLen)
	if _, err := rand.Read(dek); err != nil {
		return Sealed{}, fmt.Errorf("secrets: generate DEK: %w", err)
	}

	dekGCM, err := newGCM(dek)
	if err != nil {
		return Sealed{}, err
	}

	dataNonce, ciphertext, err := seal(dekGCM, plaintext)
	if err != nil {
		return Sealed{}, err
	}
	dekNonce, wrappedDEK, err := seal(c.kekGCM, dek)
	if err != nil {
		return Sealed{}, err
	}

	return Sealed{
		Ciphertext:   ciphertext,
		Nonce:        dataNonce,
		EncryptedDEK: wrappedDEK,
		DEKNonce:     dekNonce,
		KeyVersion:   CurrentKeyVersion,
	}, nil
}

// Decrypt unwraps the DEK with the KEK, then decrypts the secret. It returns
// an error if either authentication step fails (tampering or wrong KEK).
func (c *Cipher) Decrypt(s Sealed) ([]byte, error) {
	// GCM.Open PANICS on a wrong-length nonce, so validate before calling it. A
	// corrupt or NULL secret row (nil/short nonces) must surface as an error,
	// not crash the process.
	if len(s.DEKNonce) != c.kekGCM.NonceSize() {
		return nil, errors.New("secrets: malformed sealed secret (DEK nonce length)")
	}
	dek, err := c.kekGCM.Open(nil, s.DEKNonce, s.EncryptedDEK, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: unwrap DEK: %w", err)
	}
	dekGCM, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	if len(s.Nonce) != dekGCM.NonceSize() {
		return nil, errors.New("secrets: malformed sealed secret (data nonce length)")
	}
	plaintext, err := dekGCM.Open(nil, s.Nonce, s.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keyLen {
		return nil, errors.New("key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal generates a random nonce and returns (nonce, ciphertext).
func seal(aead cipher.AEAD, plaintext []byte) (nonce, ciphertext []byte, err error) {
	nonce = make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets: generate nonce: %w", err)
	}
	ciphertext = aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}
