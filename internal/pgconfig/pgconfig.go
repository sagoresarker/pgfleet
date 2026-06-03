// Package pgconfig is the single validation boundary for user-supplied
// PostgreSQL configuration (GUC parameters and extensions). It protects the
// platform-owned GUCs that backups and replication depend on, and constrains
// extensions to a vetted allowlist.
package pgconfig

import (
	"regexp"
	"strings"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// platformOwned GUCs are set and managed by PgFleet (backups + replication
// depend on them). Users may never override these. shared_preload_libraries is
// owned because the platform requires pg_stat_statements and merges extension
// preload libs into it (see PreloadLibraries).
var platformOwned = map[string]bool{
	"archive_mode":             true,
	"archive_command":          true,
	"wal_level":                true,
	"max_wal_senders":          true,
	"max_replication_slots":    true,
	"max_slot_wal_keep_size":   true,
	"hot_standby":              true,
	"shared_preload_libraries": true,
}

// gucKeyRe matches a syntactically valid (lowercased) GUC name.
var gucKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// statStatements is always preloaded; the metrics collector's query insights
// depend on it.
const statStatements = "pg_stat_statements"

// extension is an allowlisted extension and the preload library it needs (empty
// if it does not require one).
type extension struct {
	name           string
	preloadLibrary string
}

// allowedExtensions is the curated allowlist. The contrib extensions are present
// in the base postgres image and need no preload library. timescaledb (Community
// edition, installed in the managed image) requires its preload library, which
// PreloadLibraries merges with pg_stat_statements.
var allowedExtensions = []extension{
	{name: "pg_trgm"},
	{name: "pgcrypto"},
	{name: "uuid-ossp"},
	{name: "hstore"},
	{name: "citext"},
	{name: "timescaledb", preloadLibrary: "timescaledb"},
}

// AllowedExtensionNames returns the allowlisted extension names (for API/UI).
func AllowedExtensionNames() []string {
	names := make([]string, len(allowedExtensions))
	for i, e := range allowedExtensions {
		names[i] = e.name
	}
	return names
}

func lookupExtension(name string) (extension, bool) {
	for _, e := range allowedExtensions {
		if e.name == name {
			return e, true
		}
	}
	return extension{}, false
}

// ValidateParameters checks user-supplied GUCs: valid name, not platform-owned,
// and a sane value. GUC names are case-insensitive, so the platform-owned and
// format checks are done on the lowercased key. Postgres validates the value
// itself at startup; here we only reject empty/control-character values.
func ValidateParameters(params map[string]string) error {
	for k, v := range params {
		key := strings.ToLower(strings.TrimSpace(k))
		if !gucKeyRe.MatchString(key) {
			return apperr.New(apperr.KindInvalid, "config: invalid parameter name: "+k)
		}
		if platformOwned[key] {
			return apperr.New(apperr.KindInvalid, "config: parameter is managed by the platform and cannot be set: "+key)
		}
		if v == "" || strings.ContainsAny(v, "\n\r\x00") {
			return apperr.New(apperr.KindInvalid, "config: invalid value for parameter "+key)
		}
	}
	return nil
}

// ValidateExtensions checks each requested extension is on the allowlist.
func ValidateExtensions(exts []string) error {
	for _, e := range exts {
		if _, ok := lookupExtension(e); !ok {
			return apperr.New(apperr.KindInvalid, "config: extension not allowed: "+e)
		}
	}
	return nil
}

// PreloadLibraries returns the shared_preload_libraries list for the given
// extensions: pg_stat_statements MERGED with any preload libs the extensions
// require (never a replace).
func PreloadLibraries(exts []string) []string {
	libs := []string{statStatements}
	seen := map[string]bool{statStatements: true}
	for _, name := range exts {
		e, ok := lookupExtension(name)
		if !ok || e.preloadLibrary == "" || seen[e.preloadLibrary] {
			continue
		}
		libs = append(libs, e.preloadLibrary)
		seen[e.preloadLibrary] = true
	}
	return libs
}
