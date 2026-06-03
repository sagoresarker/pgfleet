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

// TestBackrestConfEncryptionDisabledByDefault — without BackupEncryption the
// generated config carries no cipher lines.
func TestBackrestConfEncryptionDisabledByDefault(t *testing.T) {
	p := New(docker.NewFake(), newStore(), Options{MasterKey: []byte("0123456789abcdef0123456789abcdef")})
	inst := instance.Instance{ID: "inst-1", Stanza: "orders-db", RepoType: instance.RepoLocal}
	conf, err := p.backrestConf(inst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(conf, "repo1-cipher-type") {
		t.Errorf("expected no cipher when BackupEncryption off:\n%s", conf)
	}
}

// TestBackrestConfEncryptionEnabled — with BackupEncryption on, the config
// carries the cipher lines and the derived passphrase for the instance id.
func TestBackrestConfEncryptionEnabled(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	p := New(docker.NewFake(), newStore(), Options{MasterKey: key, BackupEncryption: true})
	inst := instance.Instance{ID: "inst-1", Stanza: "orders-db", RepoType: instance.RepoLocal}
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
