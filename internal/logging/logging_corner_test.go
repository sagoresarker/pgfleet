package logging

import (
	"log/slog"
	"testing"
)

// TestParseLevelTable — level parsing is case-insensitive, trims whitespace,
// accepts "warning", and defaults unknown/empty to info.
func TestParseLevelTable(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		" warn ":  slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"ERROR":   slog.LevelError,
		"info":    slog.LevelInfo,
		"INFO":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
