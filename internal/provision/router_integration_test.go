//go:build integration

package provision

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func TestPgCatRouterRoutesQueries(t *testing.T) {
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
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.RemoveNetwork(context.Background(), netID) })

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)
	p := New(rt, repo, Options{Network: netName, InstanceHost: "localhost"})

	const pw = "router-pass-1"
	primary, _ := repo.Create(ctx, instance.NewInstance{Name: "rt-p-" + shortID(), RepoType: instance.RepoLocal, Password: pw, Role: instance.RolePrimary})
	t.Cleanup(func() { _ = p.Destroy(ctx, primary.ID, false) })
	if err := p.Provision(ctx, primary.ID, nil); err != nil {
		t.Fatalf("Provision primary: %v", err)
	}
	primary, _ = repo.Get(ctx, primary.ID)

	replica, _ := repo.Create(ctx, instance.NewInstance{Name: "rt-r-" + shortID(), RepoType: instance.RepoLocal, Password: pw, Role: instance.RoleReplica})
	t.Cleanup(func() { _ = p.Destroy(ctx, replica.ID, false) })
	if err := p.ProvisionReplica(ctx, replica.ID, primary, nil); err != nil {
		t.Fatalf("ProvisionReplica: %v", err)
	}
	replica, _ = repo.Get(ctx, replica.ID)

	// Start the router fronting primary + replica.
	routerID, routerPort, err := p.StartRouter(ctx, RouterSpec{
		ClusterID:     "test-cluster",
		ClusterName:   "rtc-" + shortID(),
		Database:      "postgres",
		User:          "postgres",
		Password:      pw,
		AdminPassword: "admin-pw",
		Members:       RouterMembersFromInstances(primary.Name, []string{replica.Name}),
	}, nil)
	if err != nil {
		t.Fatalf("StartRouter: %v", err)
	}
	t.Cleanup(func() { _ = rt.RemoveContainer(context.Background(), routerID, true) })

	routerDSN := fmt.Sprintf("postgres://postgres:%s@localhost:%d/postgres?sslmode=disable", pw, routerPort)

	// Transaction-pooling routers don't persist server-side prepared
	// statements, so clients use the simple query protocol (standard guidance
	// for pgbouncer/PgCat).
	cfg, err := pgx.ParseConfig(routerDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// Connect through the router and round-trip data (writes route to the
	// primary; reads are served by the pool).
	var conn *pgx.Conn
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err = pgx.ConnectConfig(ctx, cfg)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connect through router: %v", err)
		}
		time.Sleep(time.Second)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE TABLE routed (id int)"); err != nil {
		t.Fatalf("write through router: %v", err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO routed SELECT generate_series(1, 7)"); err != nil {
		t.Fatalf("insert through router: %v", err)
	}
	// In transaction-pooling mode PgCat load-balances reads across the primary
	// AND the replica, so a SELECT may land on the replica before it has replayed
	// the INSERT (async streaming replication lag) — returning 0 rows, or erroring
	// if even the CREATE TABLE has not arrived yet. Retry until the written rows
	// are visible through the router or we time out. The assertion is unchanged
	// (the data written through the router becomes readable through it); we only
	// allow the replica time to catch up, which is the cluster's expected behavior.
	var n int
	readDeadline := time.Now().Add(30 * time.Second)
	for {
		err := conn.QueryRow(ctx, "SELECT count(*) FROM routed").Scan(&n)
		if err == nil && n == 7 {
			break
		}
		if time.Now().After(readDeadline) {
			t.Fatalf("read through router = %d (err %v), want 7", n, err)
		}
		time.Sleep(time.Second)
	}

	// The write must have landed on the primary.
	primaryDSN, _ := p.DSN(ctx, primary.ID)
	pc, err := pgx.Connect(ctx, primaryDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close(ctx)
	var onPrimary int
	if err := pc.QueryRow(ctx, "SELECT count(*) FROM routed").Scan(&onPrimary); err != nil || onPrimary != 7 {
		t.Errorf("data not on primary: %d (err %v)", onPrimary, err)
	}
}
