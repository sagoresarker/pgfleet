// Package remotebackup captures a logical backup (pg_dump, custom format) of a
// REMOTE Postgres database that PgFleet does not manage, stores it in the
// object store, catalogs it, and can restore that captured dump into a freshly
// provisioned PgFleet target (a single instance or a primary+replica cluster).
//
// It is the engine behind the "migrate-in"/adopt flow: an operator supplies
// connection details for an external database and PgFleet pulls a portable
// snapshot into its own world.
//
// Security notes:
//   - The remote password is NEVER placed in process argv; it is passed to
//     pg_dump/pg_restore via the PGPASSWORD environment variable (mirrors the
//     SEC-6 fix in internal/api/dump.go).
//   - The password is never persisted in plaintext (the persistence layer seals
//     it with internal/secrets) and is redacted from every log line, error
//     message, and API response (see Redact and CatalogEntry, which carry a
//     redacted host only).
//   - SSRF consideration: the remote host is operator-supplied and the handlers
//     are RBAC-gated to operator/admin (a trusted role). PgFleet has no existing
//     host allow/deny convention, so we do NOT block "internal" targets here —
//     adopting a DB on a private network is a legitimate use. If a deny-list is
//     ever introduced, enforce it in RemoteConn.Validate.
package remotebackup

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

const (
	defaultPrefix = "remote-dumps/"
	// stampLayout makes lexical order equal chronological order.
	stampLayout = "20060102T150405Z"
	redactMask  = "[REDACTED]"
	defaultPort = 5432
	// captureTimeout bounds a single remote pg_dump.
	captureTimeout = 60 * time.Minute
	// restoreTimeout bounds a single pg_restore into a fresh target.
	restoreTimeout = 60 * time.Minute
)

// validSSLModes are the libpq sslmode values we accept.
var validSSLModes = map[string]bool{
	"disable": true, "allow": true, "prefer": true,
	"require": true, "verify-ca": true, "verify-full": true,
}

// RemoteConn holds the connection details for an external Postgres the operator
// wants to back up / migrate in. The Password is sensitive and must never be
// logged, echoed, or written to argv.
type RemoteConn struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// applyDefaults fills in standard defaults for optional fields.
func (c *RemoteConn) applyDefaults() {
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.SSLMode == "" {
		c.SSLMode = "prefer"
	}
}

// Validate checks the connection fields. It does not mutate the receiver.
func (c RemoteConn) Validate() error {
	switch {
	case strings.TrimSpace(c.Host) == "":
		return apperr.New(apperr.KindInvalid, "remotebackup: host is required")
	case strings.TrimSpace(c.User) == "":
		return apperr.New(apperr.KindInvalid, "remotebackup: user is required")
	case strings.TrimSpace(c.DBName) == "":
		return apperr.New(apperr.KindInvalid, "remotebackup: dbname is required")
	case c.Port < 0 || c.Port > 65535:
		return apperr.New(apperr.KindInvalid, "remotebackup: port must be between 0 and 65535")
	}
	if c.SSLMode != "" && !validSSLModes[c.SSLMode] {
		return apperr.New(apperr.KindInvalid, "remotebackup: invalid sslmode "+strconv.Quote(c.SSLMode))
	}
	return nil
}

// dsnEscape quotes a keyword/value DSN value: single quotes and backslashes are
// escaped, and the value is wrapped in single quotes when empty or containing
// whitespace/quotes so libpq parses it as one token.
func dsnEscape(v string) string {
	needsQuote := v == "" || strings.ContainsAny(v, " '\\\t\n")
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	if needsQuote {
		return "'" + v + "'"
	}
	return v
}

// DSN builds a libpq keyword/value DSN WITHOUT the password (the password is
// supplied out-of-band via PGPASSWORD). Safe to log.
func (c RemoteConn) DSN() string {
	c.applyDefaults()
	parts := []string{
		"host=" + dsnEscape(c.Host),
		"port=" + strconv.Itoa(c.Port),
		"user=" + dsnEscape(c.User),
		"dbname=" + dsnEscape(c.DBName),
		"sslmode=" + dsnEscape(c.SSLMode),
	}
	return strings.Join(parts, " ")
}

// redactHost masks a hostname for cataloging/display. It keeps the last label
// (e.g. the TLD or final segment) so operators can roughly recognize a source
// without exposing the full internal address.
func redactHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return redactMask
	}
	if i := strings.LastIndex(host, "."); i >= 0 && i < len(host)-1 {
		return redactMask + host[i:]
	}
	return redactMask
}

// Redact removes every occurrence of secret from s, replacing it with a mask.
// An empty secret is a no-op (it must never blank the whole string).
func Redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, redactMask)
}

// pgVersionRe captures the major version from `pg_dump --version` /
// `pg_restore --version` output, e.g. "(PostgreSQL) 16.2" -> 16.
var pgVersionRe = regexp.MustCompile(`\(PostgreSQL\)\s+(\d+)`)

// ParsePgDumpMajor extracts the major version from a `pg_dump --version` (or
// pg_restore) output line. ok is false when no recognizable version is present.
func ParsePgDumpMajor(out string) (int, bool) {
	m := pgVersionRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// serverMajorFromVersionNum converts a Postgres server_version_num (e.g. 160002
// for 16.2) into its major version. Since PG 10 the major is num/10000.
func serverMajorFromVersionNum(num int) int {
	return num / 10000
}

// CheckVersionSkew enforces the pg_dump compatibility rule: the dumping tool's
// major version must be >= the source server's major version. A custom-format
// dump produced by an older pg_dump can omit catalog features the newer server
// has; restoring it elsewhere is unreliable. We refuse rather than risk a
// silently-incomplete migration.
func CheckVersionSkew(dumpMajor, serverMajor int) error {
	if dumpMajor < serverMajor {
		return apperr.New(apperr.KindInvalid, fmt.Sprintf(
			"remotebackup: pg_dump major version %d is older than the remote server major version %d; "+
				"pg_dump must be >= the server. Upgrade the control-plane Postgres client tools.",
			dumpMajor, serverMajor))
	}
	return nil
}

// catalogKey returns a unique object-store key for a dump taken at t. The
// fixed-width timestamp sorts chronologically; a crypto-random suffix prevents
// same-second collisions (mirrors metabackup MB-1). It embeds NO host or
// secret.
func catalogKey(t time.Time) string {
	return defaultPrefix + "remote-" + t.UTC().Format(stampLayout) + "-" + uniqueSuffix() + ".dump"
}

// uniqueSuffix returns a short lowercase-hex random token from crypto/rand,
// falling back to the wall-clock nanoseconds if entropy is unavailable.
func uniqueSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ToLower(time.Now().UTC().Format("000000000"))
	}
	return hex.EncodeToString(b[:])
}

// buildDumpArgs constructs the pg_dump argv for a custom-format capture. The
// password is intentionally NOT included; pass it via PGPASSWORD.
func buildDumpArgs(c RemoteConn) []string {
	c.applyDefaults()
	args := []string{"--format=custom", "--no-owner", "--no-privileges"}
	args = append(args, "--host="+c.Host)
	args = append(args, "--port="+strconv.Itoa(c.Port))
	args = append(args, "--username="+c.User)
	args = append(args, "--no-password")
	args = append(args, "--dbname="+c.DBName)
	return args
}

// buildRestoreArgs constructs the pg_restore argv that loads a custom-format
// dump into a target database. Ownership/privileges from the source are dropped
// so the restore works under the fresh target's superuser. The password is
// supplied via PGPASSWORD, not argv.
func buildRestoreArgs(user, dbname, host string, port int) []string {
	if port == 0 {
		port = defaultPort
	}
	return []string{
		"--no-owner", "--no-privileges",
		"--host=" + host,
		"--port=" + strconv.Itoa(port),
		"--username=" + user,
		"--no-password",
		"--dbname=" + dbname,
	}
}
