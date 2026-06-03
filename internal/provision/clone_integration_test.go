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

// TestCloneCopiesData provisions a source instance, writes data, backs it up,
// then clones it and asserts the clone is an independent instance carrying the
// source's data (reachable with the CLONE's own password).
func TestCloneCopiesData(t *testing.T) {
	ctx := context.Background()
	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ensureManagedImage(t, rt)

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)

	source, err := repo.Create(ctx, instance.NewInstance{
		Name: "src-" + shortID(), RepoType: instance.RepoLocal, Password: "source-password-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	p := New(rt, repo, Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, source.ID, false) })
	if err := p.Provision(ctx, source.ID, nil); err != nil {
		t.Fatalf("Provision source: %v", err)
	}

	// Write data + take a full backup (clone restores from the repo).
	srcDSN, _ := p.DSN(ctx, source.ID)
	conn, err := pgx.Connect(ctx, srcDSN)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, conn, "CREATE TABLE widgets (id int)")
	mustExec(t, conn, "INSERT INTO widgets SELECT generate_series(1, 42)")
	_ = conn.Close(ctx)

	srcInst, _ := repo.Get(ctx, source.ID)
	pgbackrestBackup(t, rt, srcInst.ContainerID, srcInst.Stanza, "full")

	// Clone it.
	clone, err := repo.Create(ctx, instance.NewInstance{
		Name: "clone-" + shortID(), RepoType: instance.RepoLocal, Password: "clone-password-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Destroy(ctx, clone.ID, false) })
	if err := p.Clone(ctx, clone.ID, srcInst, nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// The clone must be reachable with ITS OWN password and carry the data.
	cloneDSN, _ := p.DSN(ctx, clone.ID)
	cconn, err := pgx.Connect(ctx, cloneDSN)
	if err != nil {
		t.Fatalf("connect clone (own password): %v", err)
	}
	defer cconn.Close(ctx)

	var count int
	if err := cconn.QueryRow(ctx, "SELECT count(*) FROM widgets").Scan(&count); err != nil {
		t.Fatalf("query clone data: %v", err)
	}
	if count != 42 {
		t.Errorf("clone widget count = %d, want 42 (data not copied)", count)
	}

	// And the clone backs up to its OWN stanza (independent repo).
	if cl, _ := repo.Get(ctx, clone.ID); cl.Stanza == srcInst.Stanza {
		t.Errorf("clone shares the source stanza %q", cl.Stanza)
	}
}
