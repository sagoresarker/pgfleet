//go:build integration

package backup

import (
	"context"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func setupCatalog(t *testing.T) (*Catalog, string) {
	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	instRepo := instance.NewRepository(pool, cipher)
	inst, err := instRepo.Create(context.Background(), instance.NewInstance{
		Name: "cat-db", RepoType: instance.RepoLocal, Password: "a-good-password",
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	return NewCatalog(pool), inst.ID
}

func bk(label, typ string, repoSize int64) pgbackrest.BackupInfo {
	return pgbackrest.BackupInfo{
		Label: label, Type: typ, RepoSize: repoSize, Size: repoSize * 2,
		WALStart: "a", WALStop: "b",
		StartTime: time.Unix(1717416000, 0).UTC(), StopTime: time.Unix(1717416060, 0).UTC(),
	}
}

func TestCatalogUpsertAndList(t *testing.T) {
	cat, instID := setupCatalog(t)
	ctx := context.Background()

	if err := cat.Upsert(ctx, instID, bk("20260603-120000F", "full", 512)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := cat.Upsert(ctx, instID, bk("20260603-130000I", "incr", 128)); err != nil {
		t.Fatal(err)
	}

	list, err := cat.List(ctx, instID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestCatalogUpsertIsIdempotentAndUpdates(t *testing.T) {
	cat, instID := setupCatalog(t)
	ctx := context.Background()

	_ = cat.Upsert(ctx, instID, bk("L1", "full", 512))
	_ = cat.Upsert(ctx, instID, bk("L1", "full", 999)) // same label, new size

	list, _ := cat.List(ctx, instID)
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1 (idempotent on label)", len(list))
	}
	if list[0].RepoSize != 999 {
		t.Errorf("repo size = %d, want updated 999", list[0].RepoSize)
	}
}

func TestCatalogDeleteRemovesSingleLabel(t *testing.T) {
	cat, instID := setupCatalog(t)
	ctx := context.Background()
	_ = cat.Upsert(ctx, instID, bk("keep", "full", 1))
	_ = cat.Upsert(ctx, instID, bk("gone", "incr", 1))

	if err := cat.Delete(ctx, instID, "gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ := cat.List(ctx, instID)
	if len(list) != 1 || list[0].Label != "keep" {
		t.Errorf("after delete = %+v, want only keep", list)
	}

	// Deleting an absent label is a no-op (idempotent), not an error.
	if err := cat.Delete(ctx, instID, "gone"); err != nil {
		t.Errorf("re-delete should be a no-op, got %v", err)
	}
	if list, _ = cat.List(ctx, instID); len(list) != 1 {
		t.Errorf("after re-delete len = %d, want 1", len(list))
	}
}

func TestCatalogPruneRemovesExpired(t *testing.T) {
	cat, instID := setupCatalog(t)
	ctx := context.Background()
	_ = cat.Upsert(ctx, instID, bk("keep", "full", 1))
	_ = cat.Upsert(ctx, instID, bk("expired", "incr", 1))

	if err := cat.Prune(ctx, instID, []string{"keep"}); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	list, _ := cat.List(ctx, instID)
	if len(list) != 1 || list[0].Label != "keep" {
		t.Errorf("after prune = %+v, want only keep", list)
	}

	// Empty keep set removes everything.
	if err := cat.Prune(ctx, instID, []string{}); err != nil {
		t.Fatal(err)
	}
	if list, _ = cat.List(ctx, instID); len(list) != 0 {
		t.Errorf("after empty prune len = %d, want 0", len(list))
	}
}
