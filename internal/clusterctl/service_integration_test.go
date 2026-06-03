//go:build integration

package clusterctl

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

var nameSeq int

func uniqueSuffix() string {
	nameSeq++
	return fmt.Sprintf("%x%d", time.Now().UnixNano()&0xffff, nameSeq)
}

func TestClusterLifecycleEndToEnd(t *testing.T) {
	ctx := context.Background()

	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	dir, _ := filepath.Abs("../../docker/postgres-pgbackrest")
	if err := rt.BuildImage(ctx, dir, instance.DefaultImage); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}

	netName := "pgfleet-cluster-net-" + uniqueSuffix()
	netID, err := rt.CreateNetwork(ctx, netName, map[string]string{docker.LabelManaged: "true"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.RemoveNetwork(context.Background(), netID) })

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	clusters := cluster.NewRepository(pool)
	instances := instance.NewRepository(pool, cipher)
	prov := provision.New(rt, instances, provision.Options{Network: netName, InstanceHost: "localhost"})
	svc := New(clusters, instances, prov, rt, instance.RepoLocal)

	const pw = "cluster-pass-1"
	c, err := svc.Create(ctx, Input{Name: "hac" + uniqueSuffix(), Replicas: 1, Password: pw})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = svc.Destroy(context.Background(), c.ID, false) })

	if err := svc.Provision(ctx, c.ID, func(s, d string) { t.Logf("cluster: %s - %s", s, d) }); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	got, _ := clusters.Get(ctx, c.ID)
	if got.Status != cluster.StatusRunning {
		t.Fatalf("status = %q, want running (err %q)", got.Status, got.LastError)
	}
	if got.PrimaryInstanceID == "" || got.RouterContainerID == "" || got.RouterPort == 0 {
		t.Fatalf("cluster not fully wired: %+v", got)
	}

	// The cluster is usable through its router.
	dsn := fmt.Sprintf("postgres://postgres:%s@localhost:%d/postgres?sslmode=disable", pw, got.RouterPort)
	cfg, _ := pgx.ParseConfig(dsn)
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	var conn *pgx.Conn
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err = pgx.ConnectConfig(ctx, cfg)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connect through cluster router: %v", err)
		}
		time.Sleep(time.Second)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE TABLE c (id int); INSERT INTO c VALUES (1),(2),(3)"); err != nil {
		t.Fatalf("write through cluster: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM c").Scan(&n); err != nil || n != 3 {
		t.Fatalf("read through cluster = %d (err %v), want 3", n, err)
	}

	// Membership: 1 primary + 1 replica.
	members, _ := instances.ListByCluster(ctx, c.ID)
	if len(members) != 2 || members[0].Role != instance.RolePrimary || members[1].Role != instance.RoleReplica {
		t.Errorf("members = %+v", members)
	}
}
