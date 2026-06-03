//go:build integration

package metabackup

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/objectstore"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func minioStore(t *testing.T, bucket string) objectstore.Config {
	t.Helper()
	m := testsupport.StartMinIO(t)
	cfg := objectstore.Config{
		Endpoint:  m.Endpoint,
		Region:    "us-east-1",
		AccessKey: m.AccessKey,
		SecretKey: m.SecretKey,
		Bucket:    bucket,
	}
	if err := objectstore.EnsureBucket(context.Background(), cfg); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}
	return cfg
}

func TestObjectStorePutGetListDelete(t *testing.T) {
	ctx := context.Background()
	cfg := minioStore(t, "meta")

	if err := objectstore.PutObject(ctx, cfg, "a/one.dump", []byte("hello")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := objectstore.PutObject(ctx, cfg, "a/two.dump", []byte("world")); err != nil {
		t.Fatalf("put: %v", err)
	}

	data, err := objectstore.GetObject(ctx, cfg, "a/one.dump")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("get = %q, want hello", data)
	}

	keys, err := objectstore.ListObjects(ctx, cfg, "a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("list returned %d keys, want 2: %v", len(keys), keys)
	}

	if err := objectstore.DeleteObject(ctx, cfg, "a/one.dump"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	keys, err = objectstore.ListObjects(ctx, cfg, "a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0] != "a/two.dump" {
		t.Fatalf("after delete, keys = %v, want [a/two.dump]", keys)
	}
}

func countRows(t *testing.T, ctx context.Context, dsn string) int {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM widgets").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestBackupListRestore(t *testing.T) {
	ctx := context.Background()
	dsn := testsupport.StartPostgres(t)
	cfg := minioStore(t, "meta")

	// Seed the meta DB with a table and rows.
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := pool.Exec(ctx, "CREATE TABLE widgets (id int primary key, name text)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO widgets VALUES (1,'a'),(2,'b'),(3,'c')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	pool.Close()

	svc := New(cfg)

	key, err := svc.Backup(ctx, dsn)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	keys, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0] != key {
		t.Fatalf("list = %v, want [%s]", keys, key)
	}

	// Mutate the data after the backup.
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM widgets"); err != nil {
		t.Fatalf("delete rows: %v", err)
	}
	pool.Close()

	if got := countRows(t, ctx, dsn); got != 0 {
		t.Fatalf("after delete count = %d, want 0", got)
	}

	if err := svc.Restore(ctx, dsn, key); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if got := countRows(t, ctx, dsn); got != 3 {
		t.Fatalf("after restore count = %d, want 3", got)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	ctx := context.Background()
	cfg := minioStore(t, "meta")
	svc := New(cfg)

	// Three dumps with distinct, chronologically increasing stamps.
	keys := []string{
		"meta-backups/pgfleet-meta-20260101T000000Z.dump",
		"meta-backups/pgfleet-meta-20260102T000000Z.dump",
		"meta-backups/pgfleet-meta-20260103T000000Z.dump",
	}
	for _, k := range keys {
		if err := objectstore.PutObject(ctx, cfg, k, []byte("x")); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}

	if err := svc.Prune(ctx, 2); err != nil {
		t.Fatalf("prune: %v", err)
	}

	got, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != keys[1] || got[1] != keys[2] {
		t.Fatalf("after prune keys = %v, want newest two %v", got, keys[1:])
	}
}
