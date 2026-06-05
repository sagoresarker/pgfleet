//go:build integration

package remotebackup

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

// memStore is an in-memory ObjectStore for the integration round-trip.
type memStore struct{ objects map[string][]byte }

func (m *memStore) Put(_ context.Context, key string, data []byte) error {
	m.objects[key] = append([]byte(nil), data...)
	return nil
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	d, ok := m.objects[key]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return d, nil
}

// memCatalog is an in-memory Catalog.
type memCatalog struct{ items []CatalogEntry }

func (m *memCatalog) Save(_ context.Context, e CatalogEntry) (CatalogEntry, error) {
	if e.ID == "" {
		e.ID = "mem-" + e.ObjectKey
	}
	m.items = append(m.items, e)
	return e, nil
}

func (m *memCatalog) Get(_ context.Context, id string) (CatalogEntry, error) {
	for _, e := range m.items {
		if e.ID == id {
			return e, nil
		}
	}
	return CatalogEntry{}, pgx.ErrNoRows
}

func (m *memCatalog) List(context.Context) ([]CatalogEntry, error) { return m.items, nil }

// connFromDSN turns a testcontainers DSN into a RemoteConn.
func connFromDSN(t *testing.T, dsn string) RemoteConn {
	t.Helper()
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	return RemoteConn{
		Host:     cfg.Host,
		Port:     int(cfg.Port),
		User:     cfg.User,
		Password: cfg.Password,
		DBName:   cfg.Database,
		SSLMode:  "disable",
	}
}

// TestCaptureAndRestoreRoundTrip captures a real remote Postgres with pg_dump
// and restores the dump into a second fresh Postgres with pg_restore, asserting
// the data survives the migrate-in flow. Requires pg_dump/pg_restore/psql on
// PATH and Docker for testcontainers.
func TestCaptureAndRestoreRoundTrip(t *testing.T) {
	for _, bin := range []string{"psql", "pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found on PATH — install postgresql-client (apt/dnf) to run this test", bin)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// "Remote" source DB with sample data.
	srcDSN := testsupport.StartPostgres(t)
	srcPool, err := pgx.Connect(ctx, srcDSN)
	if err != nil {
		t.Fatalf("connect source: %v", err)
	}
	defer srcPool.Close(ctx)
	if _, err := srcPool.Exec(ctx,
		`CREATE TABLE widgets (id int primary key, name text); INSERT INTO widgets VALUES (1,'alpha'),(2,'beta')`); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	svc := New(&memStore{objects: map[string][]byte{}}, &memCatalog{})

	entry, err := svc.Capture(ctx, connFromDSN(t, srcDSN))
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if entry.Size == 0 {
		t.Fatalf("captured dump is empty")
	}

	// Fresh target DB to restore into.
	tgtDSN := testsupport.StartPostgres(t)
	if err := svc.RestoreInto(ctx, entry.ID, tgtDSN); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Verify the data round-tripped.
	tgtPool, err := pgx.Connect(ctx, tgtDSN)
	if err != nil {
		t.Fatalf("connect target: %v", err)
	}
	defer tgtPool.Close(ctx)
	var n int
	if err := tgtPool.QueryRow(ctx, `SELECT count(*) FROM widgets`).Scan(&n); err != nil {
		t.Fatalf("query target: %v", err)
	}
	if n != 2 {
		t.Fatalf("restored row count = %d, want 2", n)
	}
}
