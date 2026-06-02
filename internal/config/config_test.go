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

func TestLoadObjectStoreAndProvisioningDefaults(t *testing.T) {
	cfg, err := Load(envMap(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultRepoType != "s3" {
		t.Errorf("DefaultRepoType default = %q, want s3", cfg.DefaultRepoType)
	}
	if cfg.DockerNetwork != "pgfleet" {
		t.Errorf("DockerNetwork default = %q, want pgfleet", cfg.DockerNetwork)
	}
	if cfg.InstanceHost != "localhost" {
		t.Errorf("InstanceHost default = %q, want localhost", cfg.InstanceHost)
	}
	if cfg.S3Region != "us-east-1" {
		t.Errorf("S3Region default = %q, want us-east-1", cfg.S3Region)
	}
}

func TestLoadObjectStoreOverrides(t *testing.T) {
	env := validEnv()
	env["PGFLEET_S3_ENDPOINT"] = "minio:9000"
	env["PGFLEET_S3_BUCKET"] = "backups"
	env["PGFLEET_S3_ACCESS_KEY"] = "AKIA"
	env["PGFLEET_S3_SECRET_KEY"] = "shhh"
	env["PGFLEET_DOCKER_NETWORK"] = "custom-net"
	env["PGFLEET_INSTANCE_HOST"] = "db.example.com"

	cfg, err := Load(envMap(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3Endpoint != "minio:9000" || cfg.S3Bucket != "backups" ||
		cfg.S3AccessKey != "AKIA" || cfg.S3SecretKey != "shhh" {
		t.Errorf("object store config not parsed: %+v", cfg)
	}
	if cfg.DockerNetwork != "custom-net" || cfg.InstanceHost != "db.example.com" {
		t.Errorf("provisioning config not parsed: %+v", cfg)
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
