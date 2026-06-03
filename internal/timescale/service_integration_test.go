//go:build integration

package timescale_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
	"github.com/sagoresarker/pgfleet/internal/timescale"
)

// TestTimescaleService_EndToEnd provisions a real managed instance with the
// timescaledb extension, builds a hypertable with data, attaches retention and
// compression policies plus a continuous aggregate, and asserts that the
// Service's read methods observe the resulting state.
//
// It requires Docker (it builds and runs the managed image) and is excluded
// from the default test run by the integration build tag.
func TestTimescaleService_EndToEnd(t *testing.T) {
	ctx := context.Background()

	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatalf("NewMoby: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	dir, err := filepath.Abs("../../docker/postgres-pgbackrest")
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.BuildImage(ctx, dir, instance.DefaultImage); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)

	inst, err := repo.Create(ctx, instance.NewInstance{
		Name:       fmt.Sprintf("ts-%x", time.Now().UnixNano()&0xffffffff),
		RepoType:   instance.RepoLocal,
		Password:   "test-password-1",
		Extensions: []string{"timescaledb"},
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	p := provision.New(rt, repo, provision.Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })
	if err := p.Provision(ctx, inst.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	dsn, err := p.DSN(ctx, inst.ID)
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Sanity: the extension is actually installed.
	var hasTS bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb')").Scan(&hasTS); err != nil {
		t.Fatal(err)
	}
	if !hasTS {
		t.Fatal("timescaledb extension was not created")
	}

	// A wide-enough time span across multiple days so retention/compression can
	// create more than one chunk (default chunk interval is 7 days).
	if _, err := conn.Exec(ctx,
		`CREATE TABLE metrics (ts timestamptz NOT NULL, device_id int NOT NULL, value double precision)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	svc := timescale.NewService()

	if err := svc.CreateHypertable(ctx, conn, "metrics", "ts"); err != nil {
		t.Fatalf("CreateHypertable: %v", err)
	}

	// Insert rows spanning ~60 days so several chunks are created.
	if _, err := conn.Exec(ctx, `
		INSERT INTO metrics (ts, device_id, value)
		SELECT now() - (g || ' days')::interval, (g % 5), random()
		FROM generate_series(0, 59) AS g`); err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	if err := svc.AddRetentionPolicy(ctx, conn, "metrics", "90 days"); err != nil {
		t.Fatalf("AddRetentionPolicy: %v", err)
	}

	if err := svc.EnableCompression(ctx, conn, "metrics", "device_id", "ts"); err != nil {
		t.Fatalf("EnableCompression: %v", err)
	}
	if err := svc.AddCompressionPolicy(ctx, conn, "metrics", "7 days"); err != nil {
		t.Fatalf("AddCompressionPolicy: %v", err)
	}

	if err := svc.CreateContinuousAggregate(ctx, conn, "metrics_hourly",
		"SELECT time_bucket('1 hour', ts) AS bucket, device_id, avg(value) AS avg_value "+
			"FROM metrics GROUP BY bucket, device_id"); err != nil {
		t.Fatalf("CreateContinuousAggregate: %v", err)
	}
	if err := svc.AddContinuousAggregatePolicy(ctx, conn, "metrics_hourly",
		"1 month", "1 hour", "1 hour"); err != nil {
		t.Fatalf("AddContinuousAggregatePolicy: %v", err)
	}

	// Assert via ListHypertables.
	hts, err := svc.ListHypertables(ctx, conn)
	if err != nil {
		t.Fatalf("ListHypertables: %v", err)
	}
	var metrics *timescale.Hypertable
	for i := range hts {
		if hts[i].Name == "metrics" {
			metrics = &hts[i]
		}
	}
	if metrics == nil {
		t.Fatalf("ListHypertables did not return the metrics hypertable; got %+v", hts)
	}
	if metrics.NumChunks <= 0 {
		t.Errorf("NumChunks = %d, want > 0", metrics.NumChunks)
	}
	if !metrics.CompressionEnabled {
		t.Error("CompressionEnabled = false, want true")
	}
	if metrics.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", metrics.SizeBytes)
	}

	// Assert via ListJobs: retention, compression, and continuous-aggregate
	// refresh policies all register background jobs.
	jobs, err := svc.ListJobs(ctx, conn)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) < 3 {
		t.Errorf("ListJobs returned %d jobs, want >= 3 (retention + compression + cagg refresh); got %+v",
			len(jobs), jobs)
	}
}
