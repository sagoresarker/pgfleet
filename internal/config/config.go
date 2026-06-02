// Package config loads and validates the PgFleet control-plane configuration
// from the process environment.
package config

import (
	"encoding/base64"
	"fmt"
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
	S3Endpoint      string
	S3Bucket        string
	S3Region        string
	S3AccessKey     string
	S3SecretKey     string
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
		S3Endpoint:             getenv("PGFLEET_S3_ENDPOINT"),
		S3Bucket:               getenv("PGFLEET_S3_BUCKET"),
		S3Region:               orDefault(getenv("PGFLEET_S3_REGION"), "us-east-1"),
		S3AccessKey:            getenv("PGFLEET_S3_ACCESS_KEY"),
		S3SecretKey:            getenv("PGFLEET_S3_SECRET_KEY"),
	}

	var err error
	if cfg.MetaDBDSN, err = required(getenv, "PGFLEET_META_DB_DSN"); err != nil {
		return nil, err
	}
	if cfg.JWTSecret, err = required(getenv, "PGFLEET_JWT_SECRET"); err != nil {
		return nil, err
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
	if v := getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is required", key)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
