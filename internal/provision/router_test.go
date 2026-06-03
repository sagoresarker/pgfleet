package provision

import (
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/pgcat"
)

func TestRouterConfigMapsMembersAndDefaults(t *testing.T) {
	cfg := routerConfig(RouterSpec{
		Database:      "postgres",
		User:          "postgres",
		Password:      "pw",
		AdminPassword: "admin-pw",
		Members: []RouterMember{
			{Host: "primary-host", Role: "primary"},
			{Host: "replica-host", Role: "replica"},
		},
	})
	if cfg.AdminUser != "pgfleet_admin" {
		t.Errorf("AdminUser = %q", cfg.AdminUser)
	}
	if cfg.ListenPort != pgcatPort {
		t.Errorf("ListenPort = %d", cfg.ListenPort)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("servers = %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Port != pgPort || cfg.Servers[0].Role != "primary" {
		t.Errorf("primary server = %+v", cfg.Servers[0])
	}
	if cfg.Servers[1].Role != "replica" {
		t.Errorf("replica server = %+v", cfg.Servers[1])
	}
	if len(cfg.Mirrors) != 0 {
		t.Errorf("expected no mirrors, got %+v", cfg.Mirrors)
	}
}

func TestRouterConfigThreadsPoolModeAndMirrors(t *testing.T) {
	cfg := routerConfig(RouterSpec{
		Database: "postgres", User: "u", Password: "p",
		PoolMode: pgcat.PoolModeSession,
		Members:  []RouterMember{{Host: "h", Role: "primary"}},
		Mirrors: []RouterMirror{
			{Host: "mirror-host"},          // default port
			{Host: "mirror-2", Port: 6000}, // explicit port
		},
	})
	if cfg.PoolMode != pgcat.PoolModeSession {
		t.Errorf("PoolMode = %q", cfg.PoolMode)
	}
	if len(cfg.Mirrors) != 2 {
		t.Fatalf("mirrors = %d", len(cfg.Mirrors))
	}
	if cfg.Mirrors[0].Port != pgPort {
		t.Errorf("default mirror port = %d, want %d", cfg.Mirrors[0].Port, pgPort)
	}
	if cfg.Mirrors[1].Port != 6000 {
		t.Errorf("explicit mirror port = %d", cfg.Mirrors[1].Port)
	}

	// The mapped config must produce a valid session-mode TOML with mirrors.
	out := pgcat.Generate(cfg)
	if !strings.Contains(out, `pool_mode = "session"`) {
		t.Errorf("generated config missing session pool_mode:\n%s", out)
	}
	if !strings.Contains(out, "[pools.postgres.shards.0.mirrors.1]") {
		t.Errorf("generated config missing mirror block:\n%s", out)
	}
}
