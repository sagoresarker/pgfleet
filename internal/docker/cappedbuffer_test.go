package docker

import (
	"strings"
	"testing"
)

// TestCappedBufferStopsAtLimit verifies a cappedBuffer accepts at most limit
// bytes, discards the rest, reports the truncation, and never reports an error
// to the writer (so the producing copy loop drains the source rather than
// aborting mid-stream). This bounds exec-output capture at the runtime layer so
// a command emitting gigabytes cannot OOM the control plane (SEC-5).
func TestCappedBufferStopsAtLimit(t *testing.T) {
	var b cappedBuffer
	b.limit = 10

	n, err := b.Write([]byte("0123456789ABCDEF"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	// Write must report it consumed the whole input (io.Copy/stdcopy treat a
	// short write as an error and would abort), but only retain limit bytes.
	if n != 16 {
		t.Fatalf("Write n = %d, want 16 (full input reported consumed)", n)
	}
	if got := b.String(); got != "0123456789" {
		t.Fatalf("buffered = %q, want %q", got, "0123456789")
	}
	if !b.Truncated() {
		t.Fatalf("Truncated() = false, want true")
	}
}

// TestCappedBufferUnderLimit keeps everything and is not truncated.
func TestCappedBufferUnderLimit(t *testing.T) {
	var b cappedBuffer
	b.limit = 100
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if b.String() != "hello" {
		t.Fatalf("buffered = %q, want hello", b.String())
	}
	if b.Truncated() {
		t.Fatalf("Truncated() = true, want false")
	}
}

// TestCappedBufferAcrossWrites verifies the limit holds across multiple writes,
// mimicking how stdcopy delivers framed chunks.
func TestCappedBufferAcrossWrites(t *testing.T) {
	var b cappedBuffer
	b.limit = 8
	for i := 0; i < 5; i++ {
		if _, err := b.Write([]byte(strings.Repeat("x", 3))); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if len(b.String()) != 8 {
		t.Fatalf("buffered %d bytes, want 8", len(b.String()))
	}
	if !b.Truncated() {
		t.Fatalf("Truncated() = false, want true")
	}
}
