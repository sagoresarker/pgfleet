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

// Config describes a PgCat router for one cluster.
type Config struct {
	ListenPort    int
	AdminUser     string
	AdminPassword string
	Database      string // the pool name / target database
	User          string // backend (and client) auth user
	Password      string // backend (and client) password
	PoolSize      int
	Servers       []Server
}

// Generate renders a pgcat.toml. Read/write splitting routes SELECTs to
// replicas and writes to the primary; primary_reads_enabled lets reads also
// fall back to the primary (e.g. when no replica is healthy).
func Generate(c Config) string {
	poolSize := c.PoolSize
	if poolSize <= 0 {
		poolSize = 20
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
	b.WriteString("\n")

	fmt.Fprintf(&b, "[pools.%s]\n", poolKey)
	b.WriteString("pool_mode = \"transaction\"\n")
	b.WriteString("query_parser_enabled = true\n")
	b.WriteString("query_parser_read_write_splitting = true\n")
	b.WriteString("primary_reads_enabled = true\n")
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
