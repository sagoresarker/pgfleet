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
	// The key carries the full chronological stamp, then a unique suffix
	// (MB-1) so same-second backups do not collide.
	wantPrefix := "meta-backups/pgfleet-meta-20260603T140509Z"
	if !strings.HasPrefix(key, wantPrefix) {
		t.Errorf("stampKey = %q, want prefix %q", key, wantPrefix)
	}
}

func TestStampKeyCustomPrefix(t *testing.T) {
	s := New(objectstore.Config{Bucket: "b"})
	s.prefix = "custom/"
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	key := s.stampKey(ts)
	wantPrefix := "custom/pgfleet-meta-20260102T030405Z"
	if !strings.HasPrefix(key, wantPrefix) {
		t.Errorf("stampKey = %q, want prefix %q", key, wantPrefix)
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

// TestStampKeyUniquePerCall is the MB-1 guarantee: two backups requested with
// the SAME timestamp (same-second resolution) must still produce distinct keys,
// so the second does not overwrite the first.
func TestStampKeyUniquePerCall(t *testing.T) {
	s := newTestService()
	ts := time.Date(2026, 6, 3, 14, 5, 9, 0, time.UTC)

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key := s.stampKey(ts)
		if seen[key] {
			t.Fatalf("stampKey produced a duplicate key %q for the same timestamp", key)
		}
		seen[key] = true

		if !strings.HasPrefix(key, "meta-backups/pgfleet-meta-20260603T140509Z") {
			t.Errorf("key %q lost its chronological stamp prefix", key)
		}
		if !strings.HasSuffix(key, ".dump") {
			t.Errorf("key %q missing .dump suffix", key)
		}
	}
}

// TestStampKeySameSecondPreservesPrefixOrdering checks that the unique suffix is
// appended AFTER the full timestamp, so keys from different seconds still sort
// chronologically regardless of their random suffixes.
func TestStampKeySameSecondPreservesPrefixOrdering(t *testing.T) {
	s := newTestService()
	earlier := s.stampKey(time.Date(2026, 6, 3, 14, 5, 9, 0, time.UTC))
	later := s.stampKey(time.Date(2026, 6, 3, 14, 5, 10, 0, time.UTC))
	if earlier >= later {
		t.Fatalf("earlier key %q should sort before later key %q", earlier, later)
	}
}

// TestParsePgDumpMajor covers extracting the major version from the
// `pg_dump --version` output, used by the integration suite to detect host vs
// server version skew.
func TestParsePgDumpMajor(t *testing.T) {
	cases := []struct {
		out  string
		want int
		ok   bool
	}{
		{"pg_dump (PostgreSQL) 16.2", 16, true},
		{"pg_dump (PostgreSQL) 15.6 (Debian 15.6-1.pgdg120+2)", 15, true},
		{"pg_dump (PostgreSQL) 17rc1", 17, true},
		{"pg_dump (PostgreSQL) 18beta1\n", 18, true},
		{"  pg_dump (PostgreSQL) 14.11  ", 14, true},
		{"pg_dump (PostgreSQL) 9.6.24", 9, true},
		{"garbage output", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parsePgDumpMajor(tc.out)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parsePgDumpMajor(%q) = (%d,%v), want (%d,%v)", tc.out, got, ok, tc.want, tc.ok)
		}
	}
}

// TestServerMajorFromVersionNum covers converting Postgres server_version_num
// (e.g. 160002) into a major version (16).
func TestServerMajorFromVersionNum(t *testing.T) {
	cases := []struct {
		num  int
		want int
	}{
		{160002, 16},
		{150006, 15},
		{170000, 17},
		{90624, 9},
		{0, 0},
	}
	for _, tc := range cases {
		if got := serverMajorFromVersionNum(tc.num); got != tc.want {
			t.Errorf("serverMajorFromVersionNum(%d) = %d, want %d", tc.num, got, tc.want)
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
