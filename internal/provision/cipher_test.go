package provision

import (
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

func TestDeriveCipherPassDeterministic(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	a := deriveCipherPass(key, "inst-1")
	b := deriveCipherPass(key, "inst-1")
	if a == "" {
		t.Fatal("expected a non-empty passphrase")
	}
	if a != b {
		t.Errorf("derivation not deterministic: %q != %q", a, b)
	}
	// HMAC-SHA256 hex is 64 chars.
	if len(a) != 64 {
		t.Errorf("expected 64 hex chars, got %d: %q", len(a), a)
	}
}

func TestDeriveCipherPassDiffersPerInstance(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	if deriveCipherPass(key, "inst-1") == deriveCipherPass(key, "inst-2") {
		t.Error("expected different instances to derive different passphrases")
	}
}

func TestDeriveCipherPassDiffersPerKey(t *testing.T) {
	if deriveCipherPass([]byte("key-a-aaaaaaaaaaaaaaaaaaaaaaaaaaa"), "inst-1") ==
		deriveCipherPass([]byte("key-b-bbbbbbbbbbbbbbbbbbbbbbbbbbb"), "inst-1") {
		t.Error("expected different master keys to derive different passphrases")
	}
}

func TestDeriveCipherPassEmptyKey(t *testing.T) {
	if got := deriveCipherPass(nil, "inst-1"); got != "" {
		t.Errorf("empty key must yield empty passphrase, got %q", got)
	}
}

// TestBackrestConfEncryptionDisabledByDefault — an unencrypted instance carries
// no cipher lines regardless of the global flag.
func TestBackrestConfEncryptionDisabledByDefault(t *testing.T) {
	p := New(docker.NewFake(), newStore(), Options{MasterKey: []byte("0123456789abcdef0123456789abcdef")})
	inst := instance.Instance{ID: "inst-1", Stanza: "orders-db", RepoType: instance.RepoLocal}
	conf, err := p.backrestConf(inst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conf, "repo1-cipher-type") {
		t.Errorf("expected no cipher for an unencrypted instance:\n%s", conf)
	}
}

// TestBackrestConfEncryptionFromInstanceState — the cipher is driven by the
// instance's persisted Encrypted flag, with the derived passphrase.
func TestBackrestConfEncryptionFromInstanceState(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	p := New(docker.NewFake(), newStore(), Options{MasterKey: key})
	inst := instance.Instance{ID: "inst-1", Stanza: "orders-db", RepoType: instance.RepoLocal, Encrypted: true}
	conf, err := p.backrestConf(inst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conf, "repo1-cipher-type=aes-256-cbc") {
		t.Errorf("expected cipher-type line:\n%s", conf)
	}
	want := "repo1-cipher-pass=" + deriveCipherPass(key, "inst-1")
	if !strings.Contains(conf, want) {
		t.Errorf("expected derived cipher pass %q:\n%s", want, conf)
	}
}

// TestBackrestConfEncryptionIgnoresGlobalFlagToggle — N1 regression: an
// ENCRYPTED instance must keep emitting the cipher even when the global
// BackupEncryption flag is now OFF (a control-plane restart with the env var
// dropped must not strip the cipher and render the repo unrecoverable). The
// inverse — an UNENCRYPTED instance with the flag now ON — must NOT gain a
// cipher (it would break reads of the existing unencrypted stanza).
func TestBackrestConfEncryptionIgnoresGlobalFlagToggle(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	// Flag now OFF, instance was created encrypted → cipher MUST still appear.
	pOff := New(docker.NewFake(), newStore(), Options{MasterKey: key, BackupEncryption: false})
	encInst := instance.Instance{ID: "inst-1", Stanza: "s", RepoType: instance.RepoLocal, Encrypted: true}
	conf, err := pOff.backrestConf(encInst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conf, "repo1-cipher-type=aes-256-cbc") {
		t.Errorf("encrypted instance lost its cipher when global flag went off:\n%s", conf)
	}

	// Flag now ON, instance was created unencrypted → cipher MUST NOT appear.
	pOn := New(docker.NewFake(), newStore(), Options{MasterKey: key, BackupEncryption: true})
	plainInst := instance.Instance{ID: "inst-2", Stanza: "s", RepoType: instance.RepoLocal, Encrypted: false}
	conf, err = pOn.backrestConf(plainInst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conf, "repo1-cipher-type") {
		t.Errorf("unencrypted instance gained a cipher when global flag went on:\n%s", conf)
	}
}
