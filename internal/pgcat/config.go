// Package pgcat generates configuration for the PgCat query router and helps
// run it as a managed container fronting a cluster's primary and replicas.
package pgcat

import (
	"fmt"
	"strings"
)

// Server is a backend Postgres in the router's shard.
type Server struct {
	Host string // container hostname on the shared network
	Port int
	Role string // "primary" or "replica"
}

// MirrorTarget is a backend that receives a shadow copy of the pool's traffic.
type MirrorTarget struct {
	Host string
	Port int
}

// Pool modes PgCat supports. Transaction pooling multiplexes a server
// connection per transaction; session pooling pins it for the client session.
const (
	PoolModeTransaction = "transaction"
	PoolModeSession     = "session"
)

// Ban/health-check defaults (seconds). PgCat bans a server that fails a
// health check and retries it after banTime, automatically routing around an
// unhealthy backend.
const (
	defaultHealthcheckTimeout = 1000 // ms
	defaultHealthcheckDelay   = 30000
	defaultBanTime            = 60 // seconds
	defaultShutdownTimeout    = 60000
)

// Config describes a PgCat router for one cluster.
type Config struct {
	ListenPort    int
	AdminUser     string
	AdminPassword string
	Database      string // the pool name / target database
	User          string // backend (and client) auth user
	Password      string // backend (and client) password
	PoolSize      int
	PoolMode      string // "transaction" (default) or "session"
	Servers       []Server
	Mirrors       []MirrorTarget // optional shadow targets; empty = no mirroring
}

// ValidatePoolMode reports whether m is an accepted pool mode. The empty string
// is accepted and means the default (transaction).
func ValidatePoolMode(m string) error {
	switch m {
	case "", PoolModeTransaction, PoolModeSession:
		return nil
	default:
		return fmt.Errorf("pgcat: invalid pool_mode %q (want %q or %q)", m, PoolModeTransaction, PoolModeSession)
	}
}

// Generate renders a pgcat.toml. Read/write splitting routes SELECTs to
// replicas and writes to the primary; primary_reads_enabled lets reads also
// fall back to the primary (e.g. when no replica is healthy).
func Generate(c Config) string {
	poolSize := c.PoolSize
	if poolSize <= 0 {
		poolSize = 20
	}

	poolMode := c.PoolMode
	if poolMode == "" {
		poolMode = PoolModeTransaction
	}

	// The pool name appears in bare TOML table headers ([pools.<name>]), where
	// quoting/escaping is not available, so it is reduced to a safe identifier.
	// Its quoted uses still go through tomlEscape.
	poolKey := tomlBareKey(c.Database)

	var b strings.Builder
	b.WriteString("[general]\n")
	b.WriteString("host = \"0.0.0.0\"\n")
	fmt.Fprintf(&b, "port = %d\n", c.ListenPort)
	b.WriteString("admin_username = \"" + tomlEscape(c.AdminUser) + "\"\n")
	b.WriteString("admin_password = \"" + tomlEscape(c.AdminPassword) + "\"\n")
	// Auto-ban unhealthy backends: PgCat health-checks idle server connections
	// and bans a server that fails, retrying it after ban_time so traffic
	// automatically routes around an unhealthy member and recovers later.
	fmt.Fprintf(&b, "healthcheck_timeout = %d\n", defaultHealthcheckTimeout)
	fmt.Fprintf(&b, "healthcheck_delay = %d\n", defaultHealthcheckDelay)
	fmt.Fprintf(&b, "ban_time = %d\n", defaultBanTime)
	fmt.Fprintf(&b, "shutdown_timeout = %d\n", defaultShutdownTimeout)
	b.WriteString("\n")

	// Count replica backends. When a pool has NO replicas — e.g. after a
	// single-replica cluster fails over and its only replica is promoted, leaving
	// a primary-only pool — read/write splitting with primary_reads_enabled=false
	// would route every SELECT to the (now empty) replica role and PgCat returns
	// AllServersDown, making the router unusable. In that case the primary must
	// serve reads too.
	replicaCount := 0
	for _, s := range c.Servers {
		if s.Role == "replica" {
			replicaCount++
		}
	}
	primaryReads := replicaCount == 0

	fmt.Fprintf(&b, "[pools.%s]\n", poolKey)
	b.WriteString("pool_mode = \"" + tomlEscape(poolMode) + "\"\n")
	// Read/write split: the query parser inspects each statement and routes
	// writes to the primary while load-balancing reads across replicas. With
	// replicas present primary_reads_enabled stays false so reads prefer the
	// replicas; with no replicas it is enabled so the primary serves reads too.
	b.WriteString("query_parser_enabled = true\n")
	b.WriteString("query_parser_read_write_splitting = true\n")
	fmt.Fprintf(&b, "primary_reads_enabled = %t\n", primaryReads)
	// load_balancing_mode accepts only "random" or "loc" (least outstanding
	// connections); "random" distributes reads across replicas. An invalid value
	// makes PgCat reject the config and exit on startup.
	b.WriteString("load_balancing_mode = \"random\"\n")
	b.WriteString("default_role = \"any\"\n")
	b.WriteString("\n")

	fmt.Fprintf(&b, "[pools.%s.users.0]\n", poolKey)
	b.WriteString("username = \"" + tomlEscape(c.User) + "\"\n")
	b.WriteString("password = \"" + tomlEscape(c.Password) + "\"\n")
	fmt.Fprintf(&b, "pool_size = %d\n", poolSize)
	b.WriteString("\n")

	fmt.Fprintf(&b, "[pools.%s.shards.0]\n", poolKey)
	b.WriteString("servers = [\n")
	for _, s := range c.Servers {
		port := s.Port
		if port == 0 {
			port = 5432
		}
		fmt.Fprintf(&b, "    [\"%s\", %d, \"%s\"],\n", tomlEscape(s.Host), port, tomlEscape(s.Role))
	}
	b.WriteString("]\n")
	b.WriteString("database = \"" + tomlEscape(c.Database) + "\"\n")

	// Query mirroring/shadowing: PgCat sends a copy of the pool's traffic to
	// each mirror target (e.g. a staging or analytics replica) without affecting
	// the client's results. Omitted entirely when no mirrors are configured.
	for i, m := range c.Mirrors {
		port := m.Port
		if port == 0 {
			port = 5432
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "[pools.%s.shards.0.mirrors.%d]\n", poolKey, i)
		b.WriteString("host = \"" + tomlEscape(m.Host) + "\"\n")
		fmt.Fprintf(&b, "port = %d\n", port)
	}

	return b.String()
}

// tomlEscape escapes a value for a TOML basic (double-quoted) string, including
// control characters (a raw newline/tab makes the file invalid TOML and is a
// vector for injecting new keys).
func tomlEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return r.Replace(s)
}

// tomlBareKey reduces a string to characters valid in a bare TOML key
// (A-Z a-z 0-9 _ -), replacing anything else with '_', so it can never inject
// table headers or keys.
func tomlBareKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}
