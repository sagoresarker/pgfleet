package pgcat

import (
	"strings"
	"testing"
)

func TestGenerateRoutesPrimaryAndReplicas(t *testing.T) {
	got := Generate(Config{
		ListenPort:    6432,
		AdminUser:     "pgcat_admin",
		AdminPassword: "admin-pw",
		Database:      "postgres",
		User:          "postgres",
		Password:      "secret",
		PoolSize:      20,
		Servers: []Server{
			{Host: "pgfleet-pg-orders-p", Port: 5432, Role: "primary"},
			{Host: "pgfleet-pg-orders-r1", Port: 5432, Role: "replica"},
		},
	})

	for _, want := range []string{
		"port = 6432",
		`admin_username = "pgcat_admin"`,
		"[pools.postgres]",
		"query_parser_read_write_splitting = true",
		`username = "postgres"`,
		`password = "secret"`,
		"pool_size = 20",
		`["pgfleet-pg-orders-p", 5432, "primary"]`,
		`["pgfleet-pg-orders-r1", 5432, "replica"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateDefaultsPoolSizeAndPort(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if !strings.Contains(got, "pool_size = 20") {
		t.Error("expected default pool_size 20")
	}
	if !strings.Contains(got, `["h", 5432, "primary"]`) {
		t.Errorf("expected default server port 5432:\n%s", got)
	}
}

func TestGenerateEscapesPasswordForTOML(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: `pa"ss\word`,
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if !strings.Contains(got, `password = "pa\"ss\\word"`) {
		t.Errorf("password not TOML-escaped:\n%s", got)
	}
}
