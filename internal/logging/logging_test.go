package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewEmitsJSONAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", &buf)

	log.Info("hello", "instance", "abc")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not JSON: %v (%q)", err, buf.String())
	}
	if entry["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", entry["msg"])
	}
	if entry["instance"] != "abc" {
		t.Errorf("instance = %v, want abc", entry["instance"])
	}
}

func TestLevelBelowThresholdIsSuppressed(t *testing.T) {
	var buf bytes.Buffer
	log := New("warn", &buf)

	log.Info("should not appear")

	if buf.Len() != 0 {
		t.Errorf("expected no output at info when level is warn, got %q", buf.String())
	}
}

func TestParseLevelDefaultsToInfo(t *testing.T) {
	if ParseLevel("nonsense") != slog.LevelInfo {
		t.Errorf("ParseLevel(nonsense) = %v, want Info", ParseLevel("nonsense"))
	}
	if ParseLevel("debug") != slog.LevelDebug {
		t.Errorf("ParseLevel(debug) = %v, want Debug", ParseLevel("debug"))
	}
	if ParseLevel("ERROR") != slog.LevelError {
		t.Errorf("ParseLevel(ERROR) = %v, want Error", ParseLevel("ERROR"))
	}
}
