package config

import (
	"strings"
	"testing"
)

// TestLoadRejectsWhitespaceOnlyRequired — a required value that is only
// whitespace is effectively unset and must be rejected, not accepted as a
// low-entropy secret / unusable DSN.
func TestLoadRejectsWhitespaceOnlyRequired(t *testing.T) {
	for _, key := range []string{"PGFLEET_META_DB_DSN", "PGFLEET_JWT_SECRET", "PGFLEET_MASTER_KEY"} {
		env := validEnv()
		env[key] = "       " // spaces only
		if _, err := Load(envMap(env)); err == nil {
			t.Errorf("whitespace-only %s should be rejected", key)
		}
	}
}

// TestLoadJWTSecretLengthBoundary — exactly 31 bytes is rejected, exactly 32 is
// accepted.
func TestLoadJWTSecretLengthBoundary(t *testing.T) {
	env := validEnv()
	env["PGFLEET_JWT_SECRET"] = strings.Repeat("a", 31)
	if _, err := Load(envMap(env)); err == nil {
		t.Error("31-byte JWT secret should be rejected")
	}
	env["PGFLEET_JWT_SECRET"] = strings.Repeat("a", 32)
	if _, err := Load(envMap(env)); err != nil {
		t.Errorf("32-byte JWT secret should be accepted: %v", err)
	}
}

// TestLoadRejectsNegativeBackupInterval — a negative duration must be rejected.
func TestLoadRejectsNegativeBackupInterval(t *testing.T) {
	env := validEnv()
	env["PGFLEET_BACKUP_INTERVAL"] = "-1h"
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Errorf("negative interval should be rejected as non-positive, got %v", err)
	}
}

// TestLoadRejectsBadMasterKey — non-base64 and wrong-length keys are rejected
// with clear errors.
func TestLoadRejectsBadMasterKey(t *testing.T) {
	env := validEnv()
	env["PGFLEET_MASTER_KEY"] = "!!!not-base64!!!"
	if _, err := Load(envMap(env)); err == nil {
		t.Error("non-base64 master key should be rejected")
	}
	// A valid base64 string that decodes to 16 bytes (wrong length).
	env["PGFLEET_MASTER_KEY"] = "MDEyMzQ1Njc4OWFiY2RlZg==" // 16 bytes
	if _, err := Load(envMap(env)); err == nil {
		t.Error("16-byte master key should be rejected (needs 32)")
	}
}
