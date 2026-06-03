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

	var b strings.Builder
	b.WriteString("[general]\n")
	b.WriteString("host = \"0.0.0.0\"\n")
	fmt.Fprintf(&b, "port = %d\n", c.ListenPort)
	b.WriteString("admin_username = \"" + tomlEscape(c.AdminUser) + "\"\n")
	b.WriteString("admin_password = \"" + tomlEscape(c.AdminPassword) + "\"\n")
	b.WriteString("\n")

	fmt.Fprintf(&b, "[pools.%s]\n", c.Database)
	b.WriteString("pool_mode = \"transaction\"\n")
	b.WriteString("query_parser_enabled = true\n")
	b.WriteString("query_parser_read_write_splitting = true\n")
	b.WriteString("primary_reads_enabled = true\n")
	b.WriteString("default_role = \"any\"\n")
	b.WriteString("\n")

	fmt.Fprintf(&b, "[pools.%s.users.0]\n", c.Database)
	b.WriteString("username = \"" + tomlEscape(c.User) + "\"\n")
	b.WriteString("password = \"" + tomlEscape(c.Password) + "\"\n")
	fmt.Fprintf(&b, "pool_size = %d\n", poolSize)
	b.WriteString("\n")

	fmt.Fprintf(&b, "[pools.%s.shards.0]\n", c.Database)
	b.WriteString("servers = [\n")
	for _, s := range c.Servers {
		port := s.Port
		if port == 0 {
			port = 5432
		}
		fmt.Fprintf(&b, "    [\"%s\", %d, \"%s\"],\n", s.Host, port, s.Role)
	}
	b.WriteString("]\n")
	fmt.Fprintf(&b, "database = \"%s\"\n", c.Database)

	return b.String()
}

// tomlEscape escapes a value for a TOML basic (double-quoted) string.
func tomlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
