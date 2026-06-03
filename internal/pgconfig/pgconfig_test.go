package pgconfig

import (
	"slices"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestValidateParametersAcceptsTuningGUCs(t *testing.T) {
	ok := map[string]string{
		"work_mem":         "8MB",
		"max_connections":  "200",
		"random_page_cost": "1.1",
	}
	if err := ValidateParameters(ok); err != nil {
		t.Errorf("valid tuning params should pass: %v", err)
	}
}

func TestValidateParametersRejectsPlatformOwned(t *testing.T) {
	// Every platform-owned GUC must be rejected, case-insensitively.
	for _, key := range []string{
		"archive_mode", "archive_command", "wal_level", "max_wal_senders",
		"max_replication_slots", "max_slot_wal_keep_size", "hot_standby",
		"shared_preload_libraries", "Archive_Mode", "WAL_LEVEL",
	} {
		err := ValidateParameters(map[string]string{key: "x"})
		if apperr.Kind(err) != apperr.KindInvalid {
			t.Errorf("platform-owned key %q must be rejected, got %v", key, apperr.Kind(err))
		}
	}
}

func TestValidateParametersRejectsBadNames(t *testing.T) {
	// Names with invalid format/characters. (Format-valid but unknown GUCs like
	// "drop" are Postgres's job to reject at startup, not the validator's.)
	for _, key := range []string{
		"", "1work", "work mem", "work-mem", "work;mem", "work.mem", "工作", "work$mem",
	} {
		err := ValidateParameters(map[string]string{key: "1"})
		if apperr.Kind(err) != apperr.KindInvalid {
			t.Errorf("invalid GUC name %q must be rejected", key)
		}
	}
}

func TestValidateParametersRejectsBadValues(t *testing.T) {
	for _, val := range []string{"", "line\nbreak", "nul\x00byte", "carriage\rreturn"} {
		err := ValidateParameters(map[string]string{"work_mem": val})
		if apperr.Kind(err) != apperr.KindInvalid {
			t.Errorf("invalid value %q must be rejected", val)
		}
	}
}

func TestValidateExtensionsAllowlist(t *testing.T) {
	if err := ValidateExtensions([]string{"pg_trgm", "pgcrypto", "uuid-ossp", "hstore", "citext"}); err != nil {
		t.Errorf("allowlisted extensions should pass: %v", err)
	}
	for _, ext := range []string{"timescaledb", "plpython3u", "evil; DROP", "", "PG_TRGM"} {
		if apperr.Kind(ValidateExtensions([]string{ext})) != apperr.KindInvalid {
			t.Errorf("non-allowlisted extension %q must be rejected", ext)
		}
	}
}

func TestPreloadLibrariesAlwaysIncludesStatStatements(t *testing.T) {
	// With no extensions, just pg_stat_statements.
	got := PreloadLibraries(nil)
	if !slices.Contains(got, "pg_stat_statements") {
		t.Errorf("preload libs must always include pg_stat_statements, got %v", got)
	}
	// Requesting non-preload extensions does not change the preload set.
	got = PreloadLibraries([]string{"pg_trgm", "citext"})
	if len(got) != 1 || got[0] != "pg_stat_statements" {
		t.Errorf("non-preload extensions should not add preload libs, got %v", got)
	}
}

// TestTimescaleDBNeedsPreload — timescaledb is allowlisted and its preload
// library is merged with (never replaces) pg_stat_statements.
func TestTimescaleDBNeedsPreload(t *testing.T) {
	if err := ValidateExtensions([]string{"timescaledb"}); err != nil {
		t.Errorf("timescaledb should be allowlisted: %v", err)
	}
	got := PreloadLibraries([]string{"timescaledb"})
	if !slices.Contains(got, "pg_stat_statements") || !slices.Contains(got, "timescaledb") {
		t.Errorf("preload libs = %v, want both pg_stat_statements and timescaledb", got)
	}
	// pg_stat_statements stays first (load order); timescaledb appended.
	if got[0] != "pg_stat_statements" {
		t.Errorf("pg_stat_statements must remain first, got %v", got)
	}
}

func TestAllowedExtensionNamesStable(t *testing.T) {
	names := AllowedExtensionNames()
	if len(names) == 0 {
		t.Fatal("AllowedExtensionNames must not be empty")
	}
	if !slices.Contains(names, "pg_trgm") {
		t.Errorf("expected pg_trgm in allowlist, got %v", names)
	}
}
