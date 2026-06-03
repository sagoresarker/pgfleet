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
		"PGFLEET_JWT_SECRET":  "super-secret-signing-key-at-least-32b",
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
	if cfg.JWTSecret != "super-secret-signing-key-at-least-32b" {
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

func TestLoadBackupScheduleDefaults(t *testing.T) {
	cfg, err := Load(envMap(validEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackupInterval.Hours() != 24 {
		t.Errorf("BackupInterval default = %v, want 24h", cfg.BackupInterval)
	}
	if cfg.BackupType != "full" {
		t.Errorf("BackupType default = %q, want full", cfg.BackupType)
	}
}

func TestLoadBackupScheduleOverridesAndRejectsBadDuration(t *testing.T) {
	env := validEnv()
	env["PGFLEET_BACKUP_INTERVAL"] = "6h"
	env["PGFLEET_BACKUP_TYPE"] = "incr"
	cfg, err := Load(envMap(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackupInterval.Hours() != 6 || cfg.BackupType != "incr" {
		t.Errorf("backup schedule not parsed: %v / %q", cfg.BackupInterval, cfg.BackupType)
	}

	env["PGFLEET_BACKUP_INTERVAL"] = "not-a-duration"
	if _, err := Load(envMap(env)); err == nil {
		t.Error("invalid backup interval should error")
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

// TestLoadValidatesInstanceNetworkingDefaults covers REG-7: the new env vars
// (bind address, restart policy, webhook URL) have secure/sane defaults applied.
func TestLoadValidatesInstanceNetworkingDefaults(t *testing.T) {
	cfg, err := Load(envMap(validEnv()))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	// Secure-by-default: bind to loopback, not 0.0.0.0.
	if cfg.InstanceBindAddress != "127.0.0.1" {
		t.Errorf("InstanceBindAddress default = %q, want 127.0.0.1", cfg.InstanceBindAddress)
	}
	if cfg.InstanceRestartPolicy != "unless-stopped" {
		t.Errorf("InstanceRestartPolicy default = %q, want unless-stopped", cfg.InstanceRestartPolicy)
	}
	if cfg.AlertWebhookURL != "" {
		t.Errorf("AlertWebhookURL default = %q, want empty", cfg.AlertWebhookURL)
	}
}

func TestLoadValidatesInstanceBindAddress(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    string
	}{
		{name: "default loopback when unset", value: "", want: "127.0.0.1"},
		{name: "explicit loopback", value: "127.0.0.1", want: "127.0.0.1"},
		{name: "private IPv4", value: "10.0.0.5", want: "10.0.0.5"},
		{name: "explicit wildcard allowed", value: "0.0.0.0", want: "0.0.0.0"},
		{name: "IPv6 loopback", value: "::1", want: "::1"},
		{name: "not an IP", value: "not-an-ip", wantErr: true},
		{name: "hostname rejected", value: "localhost", wantErr: true},
		{name: "host:port rejected", value: "127.0.0.1:5432", wantErr: true},
		{name: "whitespace rejected", value: "   ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			if tt.value != "" {
				env["PGFLEET_INSTANCE_BIND_ADDRESS"] = tt.value
			}
			cfg, err := Load(envMap(env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for bind address %q", tt.value)
				}
				if !strings.Contains(err.Error(), "PGFLEET_INSTANCE_BIND_ADDRESS") {
					t.Errorf("error %q should name the variable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.value, err)
			}
			if cfg.InstanceBindAddress != tt.want {
				t.Errorf("InstanceBindAddress = %q, want %q", cfg.InstanceBindAddress, tt.want)
			}
		})
	}
}

func TestLoadValidatesInstanceRestartPolicy(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "no", value: "no"},
		{name: "always", value: "always"},
		{name: "unless-stopped", value: "unless-stopped"},
		{name: "on-failure", value: "on-failure"},
		{name: "on-failure with count", value: "on-failure:5"},
		{name: "on-failure with zero count", value: "on-failure:0"},
		{name: "unknown policy", value: "sometimes", wantErr: true},
		{name: "on-failure bad count", value: "on-failure:abc", wantErr: true},
		{name: "on-failure negative count", value: "on-failure:-1", wantErr: true},
		{name: "on-failure empty count", value: "on-failure:", wantErr: true},
		{name: "uppercase rejected", value: "Always", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			env["PGFLEET_INSTANCE_RESTART_POLICY"] = tt.value
			cfg, err := Load(envMap(env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for restart policy %q", tt.value)
				}
				if !strings.Contains(err.Error(), "PGFLEET_INSTANCE_RESTART_POLICY") {
					t.Errorf("error %q should name the variable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.value, err)
			}
			if cfg.InstanceRestartPolicy != tt.value {
				t.Errorf("InstanceRestartPolicy = %q, want %q", cfg.InstanceRestartPolicy, tt.value)
			}
		})
	}
}

func TestLoadValidatesAlertWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "unset is fine", value: ""},
		{name: "https URL", value: "https://hooks.example.com/abc"},
		{name: "http URL", value: "http://localhost:9000/hook"},
		{name: "missing scheme", value: "hooks.example.com/abc", wantErr: true},
		{name: "non-http scheme", value: "ftp://example.com/x", wantErr: true},
		{name: "no host", value: "https://", wantErr: true},
		{name: "garbage", value: "://??", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			if tt.value != "" {
				env["PGFLEET_ALERT_WEBHOOK_URL"] = tt.value
			}
			cfg, err := Load(envMap(env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for webhook URL %q", tt.value)
				}
				if !strings.Contains(err.Error(), "PGFLEET_ALERT_WEBHOOK_URL") {
					t.Errorf("error %q should name the variable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.value, err)
			}
			if cfg.AlertWebhookURL != tt.value {
				t.Errorf("AlertWebhookURL = %q, want %q", cfg.AlertWebhookURL, tt.value)
			}
		})
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

func TestLoadBackupEncryptionDefaultsOff(t *testing.T) {
	cfg, err := Load(envMap(validEnv()))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.BackupEncryption {
		t.Error("BackupEncryption should default to false")
	}
}

func TestLoadBackupEncryptionParsesBool(t *testing.T) {
	cases := map[string]bool{
		"true":  true,
		"false": false,
		"1":     false, // only the literal "true" enables it
		"":      false,
		"TRUE":  false,
	}
	for value, want := range cases {
		env := validEnv()
		env["PGFLEET_BACKUP_ENCRYPTION"] = value
		cfg, err := Load(envMap(env))
		if err != nil {
			t.Fatalf("Load(%q) unexpected error: %v", value, err)
		}
		if cfg.BackupEncryption != want {
			t.Errorf("PGFLEET_BACKUP_ENCRYPTION=%q -> %v, want %v", value, cfg.BackupEncryption, want)
		}
	}
}
