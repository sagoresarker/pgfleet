package provision

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// cipherPassPrefix domain-separates the pgBackRest repo-cipher derivation from
// any other use of the master key, so the passphrase can never collide with a
// value derived for a different purpose.
const cipherPassPrefix = "pgbackrest-cipher:"

// deriveCipherPass returns the deterministic at-rest repository passphrase for
// an instance: hex(HMAC-SHA256(masterKey, "pgbackrest-cipher:"+instanceID)).
//
// The value is a pure function of the master key and the instance id, so it is
// stable for the life of the instance without storing it: pgBackRest fixes the
// repo cipher at stanza-create time and every later command (backup, archive,
// restore) must supply the identical passphrase or the repo is unreadable. It
// also differs per instance, so one leaked passphrase does not expose another
// instance's backups.
//
// Returns "" when masterKey is empty, so a misconfigured caller cannot enable
// encryption with a zero/derivable passphrase.
func deriveCipherPass(masterKey []byte, instanceID string) string {
	if len(masterKey) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, masterKey)
	mac.Write([]byte(cipherPassPrefix + instanceID))
	return hex.EncodeToString(mac.Sum(nil))
}
