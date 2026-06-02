//go:build integration

// Package testsupport provides shared helpers for integration tests, notably
// throwaway Postgres containers. It is only compiled under the integration
// build tag and is never part of a production build.
package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sagoresarker/pgfleet/internal/store"
)

// DefaultPostgresImage is the Postgres version the control plane targets.
const DefaultPostgresImage = "postgres:16"

// StartPostgres launches a throwaway Postgres container and returns its DSN.
// The container is terminated when the test finishes.
func StartPostgres(t *testing.T) string {
	return StartPostgresImage(t, DefaultPostgresImage)
}

// StartPostgresImage is StartPostgres for a specific image, enabling
// cross-version compatibility tests.
func StartPostgresImage(t *testing.T, image string) string {
	t.Helper()
	ctx := context.Background()

	pg, err := postgres.Run(ctx, image,
		postgres.WithDatabase("pgfleet"),
		postgres.WithUsername("pgfleet"),
		postgres.WithPassword("pgfleet"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres (%s): %v", image, err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	return dsn
}

// MigratedPool starts a Postgres container, applies all migrations, and
// returns a ready connection pool plus its DSN.
func MigratedPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()

	dsn := StartPostgres(t)
	if err := store.MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, dsn
}
