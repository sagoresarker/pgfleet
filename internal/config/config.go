// Package config loads and validates the PgFleet control-plane configuration
// from the process environment.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
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

	// BackupBlockIncr enables pgBackRest block-incremental backups (2.46+) for
	// newly provisioned instances (PGFLEET_BACKUP_BLOCK_INCR).
	BackupBlockIncr bool
	// Repo2Path, if set, configures a second (local/posix) pgBackRest repo so
	// every backup is written to both repos — 3-2-1 (PGFLEET_REPO2_PATH).
	Repo2Path string

	// Trusted-header single sign-on (Authelia / any forward-auth IdP proxy).
	// SSOEmailHeader, when set, enables POST /api/v1/auth/sso to exchange the
	// proxy-verified identity for a PgFleet token. Only set this when the API is
	// reachable ONLY through a proxy that strips client-supplied copies of these
	// headers — the header is trusted unconditionally.
	SSOEmailHeader   string
	SSOGroupsHeader  string
	SSOAutoProvision bool
	SSOAdminGroup    string
	SSOOperatorGroup string

	S3Endpoint  string
	S3Bucket    string
	S3Region    string
	S3AccessKey string
	S3SecretKey string

	// Scheduled backups.
	BackupInterval time.Duration
	BackupType     string // full | incr | diff

	// BackupEncryption enables at-rest pgBackRest repository encryption
	// (aes-256-cbc) for instances provisioned while it is true (default false).
	// pgBackRest fixes the repo cipher at stanza-create time, so this CANNOT be
	// retrofitted onto stanzas created while it was off — it only affects NEW
	// instances. Set PGFLEET_BACKUP_ENCRYPTION=true to enable.
	BackupEncryption bool
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
		BackupBlockIncr:        getenv("PGFLEET_BACKUP_BLOCK_INCR") == "true",
		Repo2Path:              getenv("PGFLEET_REPO2_PATH"),
		SSOEmailHeader:         getenv("PGFLEET_SSO_EMAIL_HEADER"),
		SSOGroupsHeader:        orDefault(getenv("PGFLEET_SSO_GROUPS_HEADER"), "Remote-Groups"),
		SSOAutoProvision:       getenv("PGFLEET_SSO_AUTO_PROVISION") == "true",
		SSOAdminGroup:          orDefault(getenv("PGFLEET_SSO_ADMIN_GROUP"), "pgfleet-admins"),
		SSOOperatorGroup:       orDefault(getenv("PGFLEET_SSO_OPERATOR_GROUP"), "pgfleet-operators"),
		S3Endpoint:             getenv("PGFLEET_S3_ENDPOINT"),
		S3Bucket:               getenv("PGFLEET_S3_BUCKET"),
		S3Region:               orDefault(getenv("PGFLEET_S3_REGION"), "us-east-1"),
		S3AccessKey:            getenv("PGFLEET_S3_ACCESS_KEY"),
		S3SecretKey:            getenv("PGFLEET_S3_SECRET_KEY"),
		BackupType:             orDefault(getenv("PGFLEET_BACKUP_TYPE"), "full"),
		BackupEncryption:       getenv("PGFLEET_BACKUP_ENCRYPTION") == "true",
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

	// REG-7: validate the instance networking / alerting env vars at load time
	// so a misconfiguration fails fast with a clear message instead of surfacing
	// later as a malformed Docker port binding, an invalid restart policy, or a
	// silently-dropped webhook delivery.
	if err := validateBindAddress(cfg.InstanceBindAddress); err != nil {
		return nil, err
	}
	if err := validateRestartPolicy(cfg.InstanceRestartPolicy); err != nil {
		return nil, err
	}
	if err := validateWebhookURL(cfg.AlertWebhookURL); err != nil {
		return nil, err
	}
	if err := validateRepo2Path(cfg.Repo2Path); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateRepo2Path accepts empty (no second repo) or an absolute, trimmed path.
// A relative/whitespace value would silently mis-target the 3-2-1 second repo, so
// it fails fast.
func validateRepo2Path(v string) error {
	if v == "" {
		return nil
	}
	if v != strings.TrimSpace(v) || !strings.HasPrefix(v, "/") {
		return fmt.Errorf("PGFLEET_REPO2_PATH: %q must be an absolute path (start with /) with no surrounding whitespace", v)
	}
	return nil
}

// validateBindAddress requires a bare IP literal (IPv4 or IPv6). Hostnames and
// host:port forms are rejected so the value can be used directly as a Docker
// port-binding host IP. 0.0.0.0 / :: are allowed only because they must be set
// explicitly (the default is the loopback 127.0.0.1, secure-by-default).
func validateBindAddress(v string) error {
	if net.ParseIP(strings.TrimSpace(v)) == nil || v != strings.TrimSpace(v) {
		return fmt.Errorf("PGFLEET_INSTANCE_BIND_ADDRESS: %q is not a valid IP address", v)
	}
	return nil
}

// validateRestartPolicy enforces Docker's allowed restart-policy names. The
// on-failure form may carry an optional non-negative retry count
// (on-failure[:N]).
func validateRestartPolicy(v string) error {
	switch v {
	case "no", "always", "unless-stopped":
		return nil
	}
	if name, count, found := strings.Cut(v, ":"); found && name == "on-failure" {
		if n, err := strconv.Atoi(count); err == nil && n >= 0 {
			return nil
		}
		return fmt.Errorf("PGFLEET_INSTANCE_RESTART_POLICY: %q has an invalid on-failure retry count", v)
	}
	if v == "on-failure" {
		return nil
	}
	return fmt.Errorf("PGFLEET_INSTANCE_RESTART_POLICY: %q is not one of no, always, unless-stopped, on-failure[:N]", v)
}

// validateWebhookURL accepts an empty value (webhooks disabled) or an absolute
// http/https URL with a host.
func validateWebhookURL(v string) error {
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("PGFLEET_ALERT_WEBHOOK_URL: %q is not a valid URL: %w", v, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("PGFLEET_ALERT_WEBHOOK_URL: %q must be an absolute http or https URL with a host", v)
	}
	return nil
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
