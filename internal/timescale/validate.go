// Package timescale manages TimescaleDB objects (hypertables, retention and
// compression policies, continuous aggregates, and background jobs) on a
// managed PostgreSQL instance.
//
// This package is the injection boundary for TimescaleDB DDL. TimescaleDB's
// management functions and DDL take object names that cannot be passed as bind
// parameters (they are either SQL identifiers or string literals embedded in
// DDL), so every name is validated against a strict format here before it is
// interpolated into SQL. Validation, not quoting, is the primary defense: a
// name that passes ValidateIdent contains only [a-zA-Z0-9_], so it can neither
// break out of a single-quoted string literal nor inject a statement.
package timescale

import (
	"strings"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// identRe matches a syntactically valid unquoted SQL identifier: 1–63 chars,
// starting with a letter or underscore, followed by letters, digits, or
// underscores. This rejects spaces, quotes, dots, hyphens, semicolons, and any
// non-ASCII (unicode) input.
const maxIdentLen = 63

// ValidateIdent reports whether name is a safe unquoted SQL identifier.
// It accepts ^[a-zA-Z_][a-zA-Z0-9_]*$ of length 1–63 and rejects everything
// else. SQL keywords (e.g. "select") are accepted because they are
// format-valid; callers that need keyword protection must quote.
func ValidateIdent(name string) error {
	if name == "" {
		return apperr.New(apperr.KindInvalid, "timescale: empty identifier")
	}
	if len(name) > maxIdentLen {
		return apperr.New(apperr.KindInvalid, "timescale: identifier too long (max 63): "+name)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 {
			if !isLetter {
				return apperr.New(apperr.KindInvalid, "timescale: invalid identifier: "+name)
			}
			continue
		}
		if !isLetter && !isDigit {
			return apperr.New(apperr.KindInvalid, "timescale: invalid identifier: "+name)
		}
	}
	return nil
}

// ValidateQualifiedName validates an optionally schema-qualified name of the
// form "table" or "schema.table". Each part must be a valid identifier.
func ValidateQualifiedName(name string) error {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return apperr.New(apperr.KindInvalid, "timescale: invalid qualified name: "+name)
	}
	for _, p := range parts {
		if err := ValidateIdent(p); err != nil {
			return apperr.New(apperr.KindInvalid, "timescale: invalid qualified name: "+name)
		}
	}
	return nil
}

// ValidateIdentList validates a comma-separated list of identifiers (used for
// compression segment_by / order_by columns). At least one identifier is
// required; surrounding whitespace per element is tolerated.
func ValidateIdentList(list string) error {
	parts := strings.Split(list, ",")
	for _, p := range parts {
		if err := ValidateIdent(strings.TrimSpace(p)); err != nil {
			return apperr.New(apperr.KindInvalid, "timescale: invalid column list: "+list)
		}
	}
	return nil
}

// ValidateInterval validates an interval-ish string destined for an INTERVAL
// literal (e.g. "7 days", "1 hour 30 minutes", "24h"). It permits only digits,
// ASCII letters, and spaces — no quotes, semicolons, parentheses, or any other
// punctuation that could break out of the literal. PostgreSQL itself validates
// the semantic correctness of the interval at execution time.
func ValidateInterval(iv string) error {
	trimmed := strings.TrimSpace(iv)
	if trimmed == "" {
		return apperr.New(apperr.KindInvalid, "timescale: empty interval")
	}
	for i := 0; i < len(iv); i++ {
		c := iv[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == ' '
		if !ok {
			return apperr.New(apperr.KindInvalid, "timescale: invalid interval: "+iv)
		}
	}
	return nil
}
