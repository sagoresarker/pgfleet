//go:build integration

package health

import (
	"context"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func TestStoreUpsertListAndDrill(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	inst, err := instance.NewRepository(pool, cipher).Create(context.Background(),
		instance.NewInstance{Name: "health-db", RepoType: instance.RepoLocal, Password: "a-good-password"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	ctx := context.Background()

	rep := Report{
		InstanceID: inst.ID, ArchivingOK: true, HasBackup: true,
		LastBackupAge: 90 * time.Minute, WALBytes: 4096,
		Issues: []string{"pg_wal is large; archiving may be stalled"}, CheckedAt: time.Now(),
	}
	if err := store.Upsert(ctx, rep); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || !list[0].ArchivingOK || len(list[0].Issues) != 1 {
		t.Fatalf("report not persisted: %+v", list)
	}
	if list[0].LastBackupAge != 90*time.Minute {
		t.Errorf("age = %v, want 90m", list[0].LastBackupAge)
	}

	// Drill update must not clobber the health fields.
	if err := store.UpdateDrill(ctx, inst.ID, true); err != nil {
		t.Fatalf("UpdateDrill: %v", err)
	}
	list, _ = store.List(ctx)
	if !list[0].DrillRan || !list[0].DrillOK {
		t.Error("drill fields not updated")
	}
	if !list[0].ArchivingOK || len(list[0].Issues) != 1 {
		t.Error("UpdateDrill clobbered health-check fields")
	}
}
