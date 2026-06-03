// Package auth provides password hashing, JWT issuance, and access control
// for the control plane.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2Params are the current argon2id cost parameters. They are encoded into
// every hash so existing hashes remain verifiable after a parameter change,
// while NeedsRehash flags them for upgrade.
type argon2Params struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

// currentParams follow the OWASP argon2id guidance (19 MiB, t=2, p=1).
var currentParams = argon2Params{
	memory:      19 * 1024,
	iterations:  2,
	parallelism: 1,
	saltLen:     16,
	keyLen:      32,
}

// ErrInvalidHash indicates a malformed or unsupported encoded hash.
var ErrInvalidHash = errors.New("auth: invalid password hash encoding")

// HashPassword hashes password with argon2id and returns a PHC-formatted
// string: $argon2id$v=19$m=...,t=...,p=...$salt$hash (base64, no padding).
func HashPassword(password string) (string, error) {
	p := currentParams
	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLen)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.iterations, p.parallelism,
		b64.EncodeToString(salt), b64.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether password matches the encoded argon2id hash.
// It returns an error for malformed encodings.
func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLen)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NeedsRehash reports whether the encoded hash was produced with parameters
// weaker than the current ones (and should be re-hashed on next login).
// Malformed encodings are reported as needing a rehash.
func NeedsRehash(encoded string) bool {
	p, _, _, err := decodeHash(encoded)
	if err != nil {
		return true
	}
	return p.memory < currentParams.memory ||
		p.iterations < currentParams.iterations ||
		p.parallelism < currentParams.parallelism ||
		p.keyLen < currentParams.keyLen
}

func decodeHash(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}

	// fmt.Sscanf stops at the format and silently ignores trailing input, so a
	// non-canonical string like "v=19garbage" would be accepted. Re-encode the
	// parsed values and require an exact match to reject any trailing garbage.
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil ||
		version != argon2.Version || fmt.Sprintf("v=%d", version) != parts[2] {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}

	var p argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil ||
		fmt.Sprintf("m=%d,t=%d,p=%d", p.memory, p.iterations, p.parallelism) != parts[3] {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	// Zero cost parameters are invalid argon2 inputs (and m=0 / keyLen=0 panics
	// deep inside the KDF). Reject them at the trust boundary.
	if p.memory == 0 || p.iterations == 0 || p.parallelism == 0 {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}

	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	hash, err := b64.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}
	// An empty salt or hash is malformed; a zero-length key makes argon2.IDKey
	// panic with a nil-pointer deref in blake2b. Reject before deriving.
	if len(salt) == 0 || len(hash) == 0 {
		return argon2Params{}, nil, nil, ErrInvalidHash
	}

	p.saltLen = uint32(len(salt))
	p.keyLen = uint32(len(hash))
	return p, salt, hash, nil
}
