//go:build integration

package provision

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

// TestPointInTimeRecovery is the acceptance gate for the backup/restore
// milestone: insert batch 1, full backup, capture a timestamp, insert batch 2,
// then PITR to the captured timestamp and assert ONLY batch 1 survives.
func TestPointInTimeRecovery(t *testing.T) {
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
	inst, err := repo.Create(ctx, instance.NewInstance{
		Name: "pitr-" + shortID(), RepoType: instance.RepoLocal, Password: "test-password-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	p := New(rt, repo, Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })
	if err := p.Provision(ctx, inst.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	got, _ := repo.Get(ctx, inst.ID)
	cid := got.ContainerID

	connect := func() *pgx.Conn {
		dsn, _ := p.DSN(ctx, inst.ID)
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		return c
	}

	// Batch 1.
	conn := connect()
	mustExec(t, conn, "CREATE TABLE t (id int)")
	mustExec(t, conn, "INSERT INTO t SELECT generate_series(1, 5)")

	// Full backup (captures batch 1).
	pgbackrestBackup(t, rt, cid, got.Stanza, "full")

	// Archive the current WAL, then capture a recovery target after batch 1.
	mustExec(t, conn, "SELECT pg_switch_wal()")
	time.Sleep(1 * time.Second)
	var target string
	if err := conn.QueryRow(ctx,
		`SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US') || '+00'`,
	).Scan(&target); err != nil {
		t.Fatalf("capture target: %v", err)
	}

	// Make the timestamps unambiguous, then batch 2 (must NOT survive PITR).
	time.Sleep(2 * time.Second)
	mustExec(t, conn, "INSERT INTO t SELECT generate_series(100, 109)")
	mustExec(t, conn, "SELECT pg_switch_wal()")
	conn.Close(ctx)
	time.Sleep(3 * time.Second) // let archive-push complete

	// PITR to the captured timestamp.
	if err := p.Restore(ctx, inst.ID, RestoreOptions{Type: "time", Target: target}, func(s, d string) {
		t.Logf("restore: %s - %s", s, d)
	}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Assert ONLY batch 1 survived.
	conn = connect()
	defer conn.Close(ctx)
	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&count); err != nil {
		t.Fatalf("count after PITR: %v", err)
	}
	if count != 5 {
		t.Fatalf("row count after PITR = %d, want 5 (only batch 1)", count)
	}
	var batch2 int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM t WHERE id >= 100").Scan(&batch2); err != nil {
		t.Fatal(err)
	}
	if batch2 != 0 {
		t.Errorf("batch 2 rows present after PITR: %d, want 0", batch2)
	}
}

func mustExec(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func pgbackrestBackup(t *testing.T, rt *docker.Moby, cid, stanza, typ string) {
	t.Helper()
	res, err := rt.Exec(context.Background(), cid, asPostgres([]string{
		"pgbackrest", "--config=" + confPath, "--stanza=" + stanza, "--type=" + typ, "backup",
	}))
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("pgbackrest backup: exit %d err %v\n%s", res.ExitCode, err, res.Stderr)
	}
}
