package store

import (
	"context"
	"testing"
	"time"
)

// TestMigrationsAreCollectable validates that the embedded migrations parse
// and register without a database — a fast guard against malformed migration
// files (bad goose annotations, duplicate versions).
func TestMigrationsAreCollectable(t *testing.T) {
	ms, err := collectMigrations()
	if err != nil {
		t.Fatalf("collectMigrations: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("expected at least one migration, got none")
	}
}

// TestMigrationVersionsAreStrictlyIncreasing guards against duplicate or
// out-of-order version numbers, which goose would apply in the wrong order.
func TestMigrationVersionsAreStrictlyIncreasing(t *testing.T) {
	ms, err := collectMigrations()
	if err != nil {
		t.Fatalf("collectMigrations: %v", err)
	}
	var prev int64
	for _, m := range ms {
		if m.Version <= prev {
			t.Fatalf("migration version %d is not strictly greater than previous %d", m.Version, prev)
		}
		prev = m.Version
	}
}

func TestOpenRejectsMalformedDSN(t *testing.T) {
	ctx := context.Background()
	if _, err := Open(ctx, "://not-a-valid-dsn"); err == nil {
		t.Fatal("Open with malformed DSN should return an error")
	}
}

func TestOpenFailsFastOnUnreachableHost(t *testing.T) {
	// Port 1 on localhost is reliably closed; the ping must fail quickly rather
	// than hang, so we bound it with a short context.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dsn := "postgres://pgfleet:pgfleet@127.0.0.1:1/pgfleet?sslmode=disable&connect_timeout=2"
	if _, err := Open(ctx, dsn); err == nil {
		t.Fatal("Open against a closed port should return an error")
	}
}
