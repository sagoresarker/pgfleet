//go:build integration

package provision

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

// ensureManagedImage builds the postgres+pgBackRest image under the default tag
// so the provisioner's EnsureImage finds it locally.
func ensureManagedImage(t *testing.T, m *docker.Moby) {
	t.Helper()
	dir, err := filepath.Abs("../../docker/postgres-pgbackrest")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.BuildImage(context.Background(), dir, instance.DefaultImage); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}
}

func TestProvisionLocalRepoEndToEnd(t *testing.T) {
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
		Name: "prov-test-" + shortID(), RepoType: instance.RepoLocal, Password: "test-password-1",
	})
	if err != nil {
		t.Fatalf("Create instance: %v", err)
	}

	p := New(rt, repo, Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })

	if err := p.Provision(ctx, inst.ID, func(step, detail string) {
		t.Logf("provision: %s - %s", step, detail)
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Status is running.
	got, _ := repo.Get(ctx, inst.ID)
	if got.Status != instance.StatusRunning {
		t.Fatalf("status = %q, want running (lastErr=%q)", got.Status, got.LastError)
	}

	// The DSN connects and the database is usable.
	dsn, err := p.DSN(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect via DSN: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE TABLE t (id int)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO t VALUES (1), (2), (3)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&n); err != nil || n != 3 {
		t.Fatalf("count = %d (err %v), want 3", n, err)
	}

	// The pgBackRest stanza exists and is healthy (check passed during provision).
	res, err := rt.Exec(ctx, got.ContainerID, asPostgres([]string{"pgbackrest", "--stanza=" + got.Stanza, "--output=json", "info"}))
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("pgbackrest info: exit %d err %v (%s)", res.ExitCode, err, res.Stderr)
	}
	if !strings.Contains(res.Stdout, got.Stanza) {
		t.Errorf("pgbackrest info missing stanza %q: %s", got.Stanza, res.Stdout)
	}
}

func TestProvisionLifecycleStartStop(t *testing.T) {
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
	inst, _ := repo.Create(ctx, instance.NewInstance{
		Name: "life-test-" + shortID(), RepoType: instance.RepoLocal, Password: "test-password-1",
	})

	p := New(rt, repo, Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })
	if err := p.Provision(ctx, inst.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if err := p.Stop(ctx, inst.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got, _ := repo.Get(ctx, inst.ID); got.Status != instance.StatusStopped {
		t.Errorf("status after stop = %q, want stopped", got.Status)
	}

	if err := p.Start(ctx, inst.ID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got, _ := repo.Get(ctx, inst.ID); got.Status != instance.StatusRunning {
		t.Errorf("status after start = %q, want running", got.Status)
	}
}

var idCounter int

func shortID() string {
	idCounter++
	return string(rune('a'+idCounter%26)) + string(rune('a'+(idCounter/26)%26))
}
