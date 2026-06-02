package config

import (
	"strings"
	"testing"
)

// envMap returns a getenv-compatible lookup backed by a map.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"PGFLEET_META_DB_DSN": "postgres://pgfleet:pgfleet@localhost:5432/pgfleet?sslmode=disable",
		"PGFLEET_MASTER_KEY":  "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", // 32 bytes base64
		"PGFLEET_JWT_SECRET":  "super-secret-signing-key",
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(envMap(validEnv()))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want \":8080\"", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want \"info\"", cfg.LogLevel)
	}
}

func TestLoadParsesRequiredValues(t *testing.T) {
	cfg, err := Load(envMap(validEnv()))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.MetaDBDSN == "" {
		t.Error("MetaDBDSN not parsed")
	}
	if cfg.JWTSecret != "super-secret-signing-key" {
		t.Errorf("JWTSecret = %q", cfg.JWTSecret)
	}
	// MasterKey must be decoded to exactly 32 raw bytes.
	if len(cfg.MasterKey) != 32 {
		t.Errorf("MasterKey length = %d, want 32", len(cfg.MasterKey))
	}
}

func TestLoadFailsOnMissingRequired(t *testing.T) {
	env := validEnv()
	delete(env, "PGFLEET_META_DB_DSN")

	_, err := Load(envMap(env))
	if err == nil {
		t.Fatal("Load() expected error for missing META_DB_DSN, got nil")
	}
	if !strings.Contains(err.Error(), "PGFLEET_META_DB_DSN") {
		t.Errorf("error %q should name the missing variable", err)
	}
}

func TestLoadFailsOnBadMasterKey(t *testing.T) {
	env := validEnv()
	env["PGFLEET_MASTER_KEY"] = "dG9vLXNob3J0" // valid base64 but only 9 bytes

	_, err := Load(envMap(env))
	if err == nil {
		t.Fatal("Load() expected error for short master key, got nil")
	}
}

func TestLoadReadsOptionalBootstrapAdmin(t *testing.T) {
	env := validEnv()
	// Absent by default.
	cfg, err := Load(envMap(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BootstrapAdminEmail != "" || cfg.BootstrapAdminPassword != "" {
		t.Errorf("bootstrap admin should be empty by default: %+v", cfg)
	}

	env["PGFLEET_BOOTSTRAP_ADMIN_EMAIL"] = "root@x.com"
	env["PGFLEET_BOOTSTRAP_ADMIN_PASSWORD"] = "change-me-please"
	cfg, err = Load(envMap(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BootstrapAdminEmail != "root@x.com" || cfg.BootstrapAdminPassword != "change-me-please" {
		t.Errorf("bootstrap admin not parsed: %+v", cfg)
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	env := validEnv()
	env["PGFLEET_HTTP_ADDR"] = ":9999"
	env["PGFLEET_LOG_LEVEL"] = "debug"

	cfg, err := Load(envMap(env))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":9999" {
		t.Errorf("HTTPAddr = %q, want \":9999\"", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want \"debug\"", cfg.LogLevel)
	}
}
