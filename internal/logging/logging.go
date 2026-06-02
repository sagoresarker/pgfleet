// Package logging provides a small structured-logging wrapper around slog,
// emitting JSON suitable for aggregation.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// New returns a JSON slog.Logger writing to w at the given level. Unknown
// level strings fall back to info.
func New(level string, w io.Writer) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)})
	return slog.New(handler)
}

// ParseLevel converts a level string (case-insensitive) to a slog.Level,
// defaulting to info for unrecognized input.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
