//go:build integration

package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/provisiontest"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func runSomeQueries(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for range 5 {
		if _, err := conn.Exec(ctx, "SELECT count(*) FROM pg_class"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreInsertQueryLatestPrune(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	inst, err := instance.NewRepository(pool, cipher).Create(context.Background(),
		instance.NewInstance{Name: "metrics-db", RepoType: instance.RepoLocal, Password: "a-good-password"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)

	samples := []Sample{
		{InstanceID: inst.ID, Metric: "connections", Value: 3, At: base.Add(-2 * time.Minute)},
		{InstanceID: inst.ID, Metric: "connections", Value: 5, At: base.Add(-1 * time.Minute)},
		{InstanceID: inst.ID, Metric: "connections", Value: 7, At: base},
		{InstanceID: inst.ID, Metric: "db_size_bytes", Value: 1024, At: base},
	}
	if err := store.Insert(ctx, samples); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.Query(ctx, inst.ID, "connections", base.Add(-90*time.Second), base.Add(time.Second))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("range query len = %d, want 2", len(got))
	}

	latest, err := store.Latest(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest["connections"].Value != 7 || latest["db_size_bytes"].Value != 1024 {
		t.Errorf("latest = %+v", latest)
	}

	if err := store.Prune(ctx, base.Add(-30*time.Second)); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	all, _ := store.Query(ctx, inst.ID, "connections", base.Add(-10*time.Minute), base.Add(time.Minute))
	if len(all) != 1 {
		t.Errorf("after prune connections len = %d, want 1", len(all))
	}
}

func TestCollectorReadsRealStats(t *testing.T) {
	inst, prov, _, _ := provisiontest.ProvisionLocalInstance(t)
	ctx := context.Background()

	dsn, err := prov.DSN(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}

	samples, err := NewCollector().Collect(ctx, inst.ID, dsn)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byMetric := map[string]float64{}
	for _, s := range samples {
		byMetric[s.Metric] = s.Value
	}
	if byMetric["connections"] < 1 {
		t.Errorf("connections = %v, want >= 1", byMetric["connections"])
	}
	if byMetric["db_size_bytes"] < 1 {
		t.Errorf("db_size_bytes = %v, want > 0", byMetric["db_size_bytes"])
	}
	if _, ok := byMetric["xact_commit"]; !ok {
		t.Error("expected xact_commit metric")
	}
	if _, ok := byMetric["checkpoints_timed"]; !ok {
		t.Error("expected checkpoints_timed metric")
	}
}

func TestTopQueriesFromStatStatements(t *testing.T) {
	inst, prov, _, _ := provisiontest.ProvisionLocalInstance(t)
	ctx := context.Background()
	dsn, _ := prov.DSN(ctx, inst.ID)

	// Generate some statement activity.
	runSomeQueries(t, dsn)

	queries, err := NewCollector().TopQueries(ctx, dsn, 10)
	if err != nil {
		t.Fatalf("TopQueries: %v", err)
	}
	if len(queries) == 0 {
		t.Error("expected at least one query from pg_stat_statements (extension enabled in provisioning)")
	}
}
