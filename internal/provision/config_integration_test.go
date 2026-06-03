//go:build integration

package provision

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

// TestProvisionAppliesUserConfig is the Area-1 acceptance test: an instance
// provisioned with a custom GUC and an extension comes up with that GUC applied
// and the extension created, AND WAL archiving still works (pgbackrest check is
// part of a successful Provision).
func TestProvisionAppliesUserConfig(t *testing.T) {
	ctx := context.Background()

	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatalf("NewMoby: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ensureManagedImage(t, rt)

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)
	inst, err := repo.Create(ctx, instance.NewInstance{
		Name: "cfg-" + shortID(), RepoType: instance.RepoLocal, Password: "test-password-1",
		Parameters: map[string]string{"work_mem": "8MB"},
		Extensions: []string{"pg_trgm"},
	})
	if err != nil {
		t.Fatal(err)
	}

	p := New(rt, repo, Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })

	// A successful Provision already runs `pgbackrest check` (WAL archiving), so
	// reaching here proves the custom config did not break backups.
	if err := p.Provision(ctx, inst.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	dsn, _ := p.DSN(ctx, inst.ID)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var workMem string
	if err := conn.QueryRow(ctx, "SHOW work_mem").Scan(&workMem); err != nil {
		t.Fatal(err)
	}
	if workMem != "8MB" {
		t.Errorf("work_mem = %q, want 8MB (user GUC not applied)", workMem)
	}

	var hasTrgm bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')").Scan(&hasTrgm); err != nil {
		t.Fatal(err)
	}
	if !hasTrgm {
		t.Error("pg_trgm extension was not created")
	}

	// pg_stat_statements must still be preloaded + present (the merge did not
	// drop it).
	var hasPSS bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')").Scan(&hasPSS); err != nil {
		t.Fatal(err)
	}
	if !hasPSS {
		t.Error("pg_stat_statements was dropped from the preload merge")
	}
}
