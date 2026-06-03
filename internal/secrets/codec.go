package secrets

import (
	"encoding/json"
	"fmt"
)

// sealedJSON is the wire form of a Sealed secret. Byte slices marshal as
// base64 strings, so the result is safe to store in a text/bytea column.
type sealedJSON struct {
	Ciphertext   []byte `json:"c"`
	Nonce        []byte `json:"n"`
	EncryptedDEK []byte `json:"ek"`
	DEKNonce     []byte `json:"en"`
	KeyVersion   int    `json:"v"`
}

// Marshal serializes a Sealed secret for storage.
func Marshal(s Sealed) ([]byte, error) {
	return json.Marshal(sealedJSON(s))
}

// Unmarshal parses a stored Sealed secret.
func Unmarshal(data []byte) (Sealed, error) {
	var j sealedJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return Sealed{}, fmt.Errorf("secrets: unmarshal sealed: %w", err)
	}
	return Sealed(j), nil
}
