// Package config loads and validates the PgFleet control-plane configuration
// from the process environment.
package config

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Config holds all runtime configuration for the control plane.
type Config struct {
	// HTTPAddr is the listen address for the API server.
	HTTPAddr string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// MetaDBDSN is the connection string for the control-plane meta database.
	MetaDBDSN string
	// JWTSecret signs and verifies auth tokens.
	JWTSecret string
	// MasterKey is the 32-byte key-encryption key for secrets at rest.
	MasterKey []byte
	// BootstrapAdminEmail/Password optionally seed the first admin user on an
	// empty database. Both empty disables bootstrapping.
	BootstrapAdminEmail    string
	BootstrapAdminPassword string

	// Provisioning / object-store settings for managed instances.
	DefaultRepoType string // "s3" | "local"
	DockerNetwork   string // network managed instances join (to reach MinIO)
	InstanceHost    string // host clients use in connection strings
	// InstanceRestartPolicy is the Docker restart policy applied to managed
	// instance + router containers so they survive a daemon/host restart.
	InstanceRestartPolicy string
	// InstanceBindAddress is the host interface published instance/router ports
	// bind to. Defaults to 127.0.0.1 (secure-by-default); set to a private IP
	// or 0.0.0.0 to expose more broadly.
	InstanceBindAddress string
	// AlertWebhookURL, if set, receives a JSON POST on every alert transition.
	AlertWebhookURL string
	// AutoFailover enables the in-house cluster failover controller (default
	// true). Set PGFLEET_AUTO_FAILOVER=false to disable automatic promotion.
	AutoFailover bool
	S3Endpoint   string
	S3Bucket     string
	S3Region     string
	S3AccessKey  string
	S3SecretKey  string

	// Scheduled backups.
	BackupInterval time.Duration
	BackupType     string // full | incr | diff
}

// Load reads configuration using the provided getenv function (typically
// os.Getenv). It applies defaults for optional values and returns an error
// naming the first required value that is missing or invalid.
func Load(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		HTTPAddr:               orDefault(getenv("PGFLEET_HTTP_ADDR"), ":8080"),
		LogLevel:               orDefault(getenv("PGFLEET_LOG_LEVEL"), "info"),
		BootstrapAdminEmail:    getenv("PGFLEET_BOOTSTRAP_ADMIN_EMAIL"),
		BootstrapAdminPassword: getenv("PGFLEET_BOOTSTRAP_ADMIN_PASSWORD"),
		DefaultRepoType:        orDefault(getenv("PGFLEET_DEFAULT_REPO_TYPE"), "s3"),
		DockerNetwork:          orDefault(getenv("PGFLEET_DOCKER_NETWORK"), "pgfleet"),
		InstanceHost:           orDefault(getenv("PGFLEET_INSTANCE_HOST"), "localhost"),
		InstanceRestartPolicy:  orDefault(getenv("PGFLEET_INSTANCE_RESTART_POLICY"), "unless-stopped"),
		InstanceBindAddress:    orDefault(getenv("PGFLEET_INSTANCE_BIND_ADDRESS"), "127.0.0.1"),
		AlertWebhookURL:        getenv("PGFLEET_ALERT_WEBHOOK_URL"),
		AutoFailover:           getenv("PGFLEET_AUTO_FAILOVER") != "false",
		S3Endpoint:             getenv("PGFLEET_S3_ENDPOINT"),
		S3Bucket:               getenv("PGFLEET_S3_BUCKET"),
		S3Region:               orDefault(getenv("PGFLEET_S3_REGION"), "us-east-1"),
		S3AccessKey:            getenv("PGFLEET_S3_ACCESS_KEY"),
		S3SecretKey:            getenv("PGFLEET_S3_SECRET_KEY"),
		BackupType:             orDefault(getenv("PGFLEET_BACKUP_TYPE"), "full"),
	}

	var err error
	if cfg.BackupInterval, err = parseDuration(getenv("PGFLEET_BACKUP_INTERVAL"), 24*time.Hour); err != nil {
		return nil, err
	}
	if cfg.BackupInterval <= 0 {
		return nil, fmt.Errorf("PGFLEET_BACKUP_INTERVAL must be positive")
	}
	if cfg.MetaDBDSN, err = required(getenv, "PGFLEET_META_DB_DSN"); err != nil {
		return nil, err
	}
	if cfg.JWTSecret, err = required(getenv, "PGFLEET_JWT_SECRET"); err != nil {
		return nil, err
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("PGFLEET_JWT_SECRET must be at least 32 bytes")
	}

	rawKey, err := required(getenv, "PGFLEET_MASTER_KEY")
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(rawKey)
	if err != nil {
		return nil, fmt.Errorf("PGFLEET_MASTER_KEY: invalid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("PGFLEET_MASTER_KEY: must decode to 32 bytes, got %d", len(key))
	}
	cfg.MasterKey = key

	return cfg, nil
}

func required(getenv func(string) string, key string) (string, error) {
	// A value that is only whitespace is effectively unset (a low-entropy
	// "secret" or an unusable DSN), so reject it. Return the original value
	// unchanged so legitimately-spaced secrets are preserved.
	if v := getenv(key); strings.TrimSpace(v) != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is required", key)
}

func parseDuration(v string, def time.Duration) (time.Duration, error) {
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("PGFLEET_BACKUP_INTERVAL: %w", err)
	}
	return d, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
