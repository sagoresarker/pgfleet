//go:build integration

package provision

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func TestStreamingReplication(t *testing.T) {
	ctx := context.Background()
	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ensureManagedImage(t, rt)

	netName := "pgfleet-test-net-" + shortID()
	netID, err := rt.CreateNetwork(ctx, netName, map[string]string{docker.LabelManaged: "true"})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	t.Cleanup(func() { _ = rt.RemoveNetwork(context.Background(), netID) })

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)
	p := New(rt, repo, Options{Network: netName, InstanceHost: "localhost"})

	// A password with a space + special chars exercises the .pgpass path
	// (would silently break an inline-conninfo password).
	const pw = "rep secret:1#pw"

	// Primary.
	primary, err := repo.Create(ctx, instance.NewInstance{
		Name: "rep-p-" + shortID(), RepoType: instance.RepoLocal, Password: pw, Role: instance.RolePrimary,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Destroy(ctx, primary.ID, false) })
	if err := p.Provision(ctx, primary.ID, nil); err != nil {
		t.Fatalf("Provision primary: %v", err)
	}
	primary, _ = repo.Get(ctx, primary.ID)

	// Replica (same password — it is a physical copy of the primary).
	replica, err := repo.Create(ctx, instance.NewInstance{
		Name: "rep-r-" + shortID(), RepoType: instance.RepoLocal, Password: pw, Role: instance.RoleReplica,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Destroy(ctx, replica.ID, false) })
	if err := p.ProvisionReplica(ctx, replica.ID, primary, func(s, d string) { t.Logf("replica: %s - %s", s, d) }); err != nil {
		t.Fatalf("ProvisionReplica: %v", err)
	}

	// Write on the primary.
	primaryDSN, _ := p.DSN(ctx, primary.ID)
	pc, err := pgx.Connect(ctx, primaryDSN)
	if err != nil {
		t.Fatalf("connect primary: %v", err)
	}
	defer pc.Close(ctx)
	if _, err := pc.Exec(ctx, "CREATE TABLE repl_t (id int)"); err != nil {
		t.Fatalf("create on primary: %v", err)
	}
	if _, err := pc.Exec(ctx, "INSERT INTO repl_t SELECT generate_series(1, 42)"); err != nil {
		t.Fatalf("insert on primary: %v", err)
	}

	// Read on the replica — the data must have replicated.
	replicaDSN, _ := p.DSN(ctx, replica.ID)
	var rc *pgx.Conn
	var count int
	deadline := time.Now().Add(30 * time.Second)
	for {
		rc, err = pgx.Connect(ctx, replicaDSN)
		if err == nil {
			err = rc.QueryRow(ctx, "SELECT count(*) FROM repl_t").Scan(&count)
		}
		if err == nil && count == 42 {
			break
		}
		if rc != nil {
			rc.Close(ctx)
		}
		if time.Now().After(deadline) {
			t.Fatalf("replica did not catch up: count=%d err=%v", count, err)
		}
		time.Sleep(time.Second)
	}
	defer rc.Close(ctx)

	// The replica is a read-only standby.
	var inRecovery bool
	if err := rc.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil || !inRecovery {
		t.Errorf("replica should be in recovery (standby): %v / %v", inRecovery, err)
	}
	if _, err := rc.Exec(ctx, "INSERT INTO repl_t VALUES (999)"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("write to replica should fail as read-only, got %v", err)
	}
}
