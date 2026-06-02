package secrets

import (
	"bytes"
	"testing"
)

func TestSealedMarshalRoundTrip(t *testing.T) {
	c, _ := New(make([]byte, 32))
	sealed, err := c.Encrypt([]byte("password123"))
	if err != nil {
		t.Fatal(err)
	}

	blob, err := Marshal(sealed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	back, err := Unmarshal(blob)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	got, err := c.Decrypt(back)
	if err != nil {
		t.Fatalf("Decrypt after round-trip: %v", err)
	}
	if !bytes.Equal(got, []byte("password123")) {
		t.Errorf("round-trip secret = %q", got)
	}
}

func TestUnmarshalRejectsGarbage(t *testing.T) {
	if _, err := Unmarshal([]byte("not-json")); err == nil {
		t.Error("Unmarshal of garbage should error")
	}
}
