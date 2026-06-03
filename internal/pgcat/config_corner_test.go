package pgcat

import (
	"strings"
	"testing"
)

// TestGenerateDatabaseCannotInjectTOML — a Database value containing TOML
// metacharacters must not create new tables/keys via the section headers or
// the database= value.
func TestGenerateDatabaseCannotInjectTOML(t *testing.T) {
	got := Generate(Config{
		Database: "pg\"]\n[evil]\nx=1\n",
		User:     "u", Password: "p",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if strings.Contains(got, "\n[evil]") {
		t.Errorf("database value injected a new TOML table:\n%s", got)
	}
	if strings.Contains(got, "\nx=1") {
		t.Errorf("database value injected a new key:\n%s", got)
	}
}

// TestGenerateHostRoleCannotInjectArray — a malicious Host or Role must not
// break out of the servers array row.
func TestGenerateHostRoleCannotInjectArray(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "p",
		Servers: []Server{{Host: "h\", 1, \"primary\"], [\"evil", Port: 5432, Role: "primary\"]\n[evil2]"}},
	})
	// A real (unescaped) newline introducing a new table would be injection.
	if strings.Contains(got, "\n[evil2]") {
		t.Errorf("role injected a new table:\n%s", got)
	}
	// The embedded quotes must be escaped (backslash-quote), proving the host
	// and role values stayed inside their string elements.
	if !strings.Contains(got, `\"`) {
		t.Errorf("host/role quotes were not escaped:\n%s", got)
	}
}

// TestGeneratePasswordNewlineEscaped — a password with a newline must be
// escaped, not emitted raw (a raw newline in a TOML basic string is invalid
// and would stop the router from starting).
func TestGeneratePasswordNewlineEscaped(t *testing.T) {
	got := Generate(Config{
		Database: "postgres", User: "u", Password: "pa\nss",
		Servers: []Server{{Host: "h", Role: "primary"}},
	})
	if strings.Contains(got, "password = \"pa\nss\"") {
		t.Errorf("password newline emitted raw:\n%s", got)
	}
	if !strings.Contains(got, `pa\nss`) {
		t.Errorf("password newline should be escaped as \\n:\n%s", got)
	}
}
