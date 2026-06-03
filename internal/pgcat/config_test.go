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

func TestGeneratePoolModeDefaultsToTransaction(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if !strings.Contains(got, `pool_mode = "transaction"`) {
		t.Errorf("expected default pool_mode transaction:\n%s", got)
	}
}

func TestGeneratePoolModeSession(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p", PoolMode: "session",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if !strings.Contains(got, `pool_mode = "session"`) {
		t.Errorf("expected pool_mode session:\n%s", got)
	}
}

func TestValidatePoolMode(t *testing.T) {
	for _, m := range []string{"", "transaction", "session"} {
		if err := ValidatePoolMode(m); err != nil {
			t.Errorf("ValidatePoolMode(%q) = %v, want nil", m, err)
		}
	}
	for _, m := range []string{"statement", "Transaction", "bogus"} {
		if err := ValidatePoolMode(m); err == nil {
			t.Errorf("ValidatePoolMode(%q) = nil, want error", m)
		}
	}
}

func TestGenerateReadWriteSplitAndRoles(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{
			{Host: "p1", Port: 5432, Role: "primary"},
			{Host: "r1", Port: 5432, Role: "replica"},
			{Host: "r2", Port: 5432, Role: "replica"},
		},
	})
	for _, want := range []string{
		"query_parser_enabled = true",
		"query_parser_read_write_splitting = true",
		"primary_reads_enabled = false",
		`load_balancing_mode = "loadbalancing"`,
		`["p1", 5432, "primary"]`,
		`["r1", 5432, "replica"]`,
		`["r2", 5432, "replica"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateHealthCheckAndBanConfig(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	for _, want := range []string{
		"healthcheck_timeout =",
		"healthcheck_delay =",
		"ban_time = 60",
		"shutdown_timeout =",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateMirrorsWhenSet(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{{Host: "h", Role: "primary"}},
		Mirrors: []MirrorTarget{
			{Host: "mirror-a", Port: 5432},
			{Host: "mirror-b", Port: 5433},
		},
	})
	for _, want := range []string{
		"[pools.postgres.shards.0.mirrors.0]",
		`host = "mirror-a"`,
		"port = 5432",
		"[pools.postgres.shards.0.mirrors.1]",
		`host = "mirror-b"`,
		"port = 5433",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateNoMirrorBlockWhenEmpty(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if strings.Contains(got, "mirrors") {
		t.Errorf("expected no mirror block when empty:\n%s", got)
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
