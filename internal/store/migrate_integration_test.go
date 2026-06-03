//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPostgres spins a throwaway Postgres and returns its DSN.
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	pg, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("pgfleet"),
		postgres.WithUsername("pgfleet"),
		postgres.WithPassword("pgfleet"),
		// Wait for the readiness log TWICE: Postgres opens the port during the
		// initdb temporary server and then restarts, so a port-only wait can
		// connect mid-restart and get "connection reset by peer". Requiring two
		// "ready" log lines (temp server + real server) plus a port check makes
		// the container genuinely ready before tests connect. This mirrors
		// testsupport.StartPostgres, which store cannot import (it would create
		// a cycle, since testsupport imports store).
		testcontainers.WithWaitStrategy(
			wait.ForAll(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
				wait.ForListeningPort("5432/tcp"),
			).WithDeadline(120*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return dsn
}

func extensionExists(t *testing.T, dsn, name string) bool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	var exists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, name,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query pg_extension: %v", err)
	}
	return exists
}

func TestMigrateUpThenDown(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()

	if err := MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if !extensionExists(t, dsn, "pgcrypto") {
		t.Error("after MigrateUp, pgcrypto extension should exist")
	}

	if err := MigrateDownTo(ctx, dsn, 0); err != nil {
		t.Fatalf("MigrateDownTo: %v", err)
	}
	if extensionExists(t, dsn, "pgcrypto") {
		t.Error("after MigrateDownTo(0), pgcrypto extension should be gone")
	}
}

func TestOpenPingsSuccessfully(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()

	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	if err := Ready(pool)(ctx); err != nil {
		t.Errorf("Ready check failed: %v", err)
	}
}

// TestMigrateUpIsIdempotent verifies that re-running MigrateUp on an
// already-migrated database is a no-op and does not error — important because
// the control plane runs migrations on every boot.
func TestMigrateUpIsIdempotent(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()

	if err := MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("first MigrateUp: %v", err)
	}
	first, err := Version(ctx, dsn)
	if err != nil {
		t.Fatalf("Version after first up: %v", err)
	}
	if first <= 0 {
		t.Fatalf("expected a recorded version > 0, got %d", first)
	}

	if err := MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("second MigrateUp should be a no-op: %v", err)
	}
	second, err := Version(ctx, dsn)
	if err != nil {
		t.Fatalf("Version after second up: %v", err)
	}
	if second != first {
		t.Errorf("version changed on idempotent re-up: %d -> %d", first, second)
	}
}

// TestMigrateDownThenUpCycle verifies the schema can be fully torn down and
// rebuilt — the basis for clean test fixtures and disaster rebuilds.
func TestMigrateDownThenUpCycle(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()

	if err := MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if err := MigrateDownTo(ctx, dsn, 0); err != nil {
		t.Fatalf("MigrateDownTo(0): %v", err)
	}
	if v, err := Version(ctx, dsn); err != nil || v != 0 {
		t.Fatalf("version after full down = %d (err %v), want 0", v, err)
	}
	if err := MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("re-MigrateUp after down: %v", err)
	}
	if !extensionExists(t, dsn, "pgcrypto") {
		t.Error("pgcrypto should exist again after re-up")
	}
}
