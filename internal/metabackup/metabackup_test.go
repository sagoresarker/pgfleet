package metabackup

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/objectstore"
)

func newTestService() *Service {
	return New(objectstore.Config{
		Endpoint:  "minio:9000",
		Region:    "us-east-1",
		AccessKey: "k",
		SecretKey: "s",
		Bucket:    "b",
	})
}

func TestStampKeyFormat(t *testing.T) {
	s := newTestService()
	ts := time.Date(2026, 6, 3, 14, 5, 9, 0, time.UTC)
	key := s.stampKey(ts)

	if !strings.HasPrefix(key, "meta-backups/") {
		t.Errorf("key %q missing meta-backups/ prefix", key)
	}
	if !strings.HasSuffix(key, ".dump") {
		t.Errorf("key %q missing .dump suffix", key)
	}
	want := "meta-backups/pgfleet-meta-20260603T140509Z.dump"
	if key != want {
		t.Errorf("stampKey = %q, want %q", key, want)
	}
}

func TestStampKeyCustomPrefix(t *testing.T) {
	s := New(objectstore.Config{Bucket: "b"})
	s.prefix = "custom/"
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	key := s.stampKey(ts)
	want := "custom/pgfleet-meta-20260102T030405Z.dump"
	if key != want {
		t.Errorf("stampKey = %q, want %q", key, want)
	}
}

// TestStampKeyLexicalOrderMatchesChronological is the core ordering guarantee:
// keys sorted as strings must be in the same order as the timestamps that
// produced them, so List + Prune can treat lexical order as chronological.
func TestStampKeyLexicalOrderMatchesChronological(t *testing.T) {
	s := newTestService()
	times := []time.Time{
		time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		time.Date(2026, 6, 3, 14, 5, 9, 0, time.UTC),
		time.Date(2026, 6, 3, 14, 5, 10, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	keys := make([]string, len(times))
	for i, ts := range times {
		keys[i] = s.stampKey(ts)
	}

	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)

	for i := range keys {
		if sorted[i] != keys[i] {
			t.Fatalf("lexical order != chronological order:\n got  %v\n want %v", sorted, keys)
		}
	}
}

func TestNewDefaultPrefix(t *testing.T) {
	s := newTestService()
	if s.prefix != "meta-backups/" {
		t.Errorf("default prefix = %q, want meta-backups/", s.prefix)
	}
	if s.now == nil {
		t.Error("now func should be set")
	}
}
