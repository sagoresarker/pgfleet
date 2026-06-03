//go:build integration

package alerts

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func TestSyncFireThenResolveRoundTrip(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()

	cipher, _ := secrets.New(make([]byte, 32))
	inst, err := instance.NewRepository(pool, cipher).Create(ctx,
		instance.NewInstance{Name: "alerts-db", RepoType: instance.RepoLocal, Password: "a-good-password"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	store := NewStore(pool)

	// First sync: one firing finding -> one firing transition.
	findings := Evaluate(Snapshot{InstanceID: inst.ID, DiskFreePercent: ptr(2)}, DefaultThresholds())
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	trs, err := store.Sync(ctx, inst.ID, findings)
	if err != nil {
		t.Fatalf("Sync (fire): %v", err)
	}
	if len(trs) != 1 || trs[0].To != StateFiring || trs[0].From != "" {
		t.Fatalf("fire transitions = %+v", trs)
	}
	if trs[0].Kind != KindDiskFull || trs[0].Severity != SeverityCritical {
		t.Errorf("fire transition fields = %+v", trs[0])
	}

	active, err := store.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].State != StateFiring || active[0].Kind != KindDiskFull {
		t.Fatalf("ListActive = %+v", active)
	}
	if active[0].InstanceID != inst.ID {
		t.Errorf("active instance id = %q, want %q", active[0].InstanceID, inst.ID)
	}

	// Second sync with the same finding: no transition (idempotent upsert).
	trs, err = store.Sync(ctx, inst.ID, findings)
	if err != nil {
		t.Fatalf("Sync (repeat): %v", err)
	}
	if len(trs) != 0 {
		t.Errorf("repeat sync should produce no transitions, got %+v", trs)
	}
	active, _ = store.ListActive(ctx)
	if len(active) != 1 {
		t.Errorf("repeat sync should not duplicate the alert, len = %d", len(active))
	}

	// Third sync with no findings: the firing alert resolves.
	trs, err = store.Sync(ctx, inst.ID, nil)
	if err != nil {
		t.Fatalf("Sync (resolve): %v", err)
	}
	if len(trs) != 1 || trs[0].From != StateFiring || trs[0].To != StateResolved {
		t.Fatalf("resolve transitions = %+v", trs)
	}

	active, _ = store.ListActive(ctx)
	if len(active) != 0 {
		t.Errorf("after resolve, no alert should be active, got %+v", active)
	}

	// History for the instance still shows the (now resolved) alert.
	all, err := store.ListForInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("ListForInstance: %v", err)
	}
	if len(all) != 1 || all[0].State != StateResolved || all[0].ResolvedAt == nil {
		t.Fatalf("ListForInstance = %+v", all)
	}

	// A fresh firing on the same kind opens a new alert (the partial unique index
	// only constrains firing rows, so a resolved row does not block it).
	trs, err = store.Sync(ctx, inst.ID, findings)
	if err != nil {
		t.Fatalf("Sync (re-fire): %v", err)
	}
	if len(trs) != 1 || trs[0].To != StateFiring {
		t.Fatalf("re-fire transitions = %+v", trs)
	}
	all, _ = store.ListForInstance(ctx, inst.ID)
	if len(all) != 2 {
		t.Errorf("history should now have 2 rows, got %d", len(all))
	}
}

func TestSyncMultipleKindsAndPartialResolve(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()

	cipher, _ := secrets.New(make([]byte, 32))
	inst, err := instance.NewRepository(pool, cipher).Create(ctx,
		instance.NewInstance{Name: "alerts-multi", RepoType: instance.RepoLocal, Password: "a-good-password"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	store := NewStore(pool)
	th := DefaultThresholds()

	// Two simultaneous problems.
	snap := Snapshot{
		InstanceID:                   inst.ID,
		DiskFreePercent:              ptr(3),
		ConnectionUtilizationPercent: ptr(95),
	}
	trs, err := store.Sync(ctx, inst.ID, Evaluate(snap, th))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(trs) != 2 {
		t.Fatalf("expected 2 firing transitions, got %d: %+v", len(trs), trs)
	}
	if active, _ := store.ListActive(ctx); len(active) != 2 {
		t.Fatalf("expected 2 active alerts, got %d", len(active))
	}

	// Disk recovers, connections still saturated: exactly one resolve transition.
	snap.DiskFreePercent = ptr(80)
	trs, err = store.Sync(ctx, inst.ID, Evaluate(snap, th))
	if err != nil {
		t.Fatalf("Sync (partial resolve): %v", err)
	}
	if len(trs) != 1 || trs[0].Kind != KindDiskFull || trs[0].To != StateResolved {
		t.Fatalf("partial resolve transitions = %+v", trs)
	}
	active, _ := store.ListActive(ctx)
	if len(active) != 1 || active[0].Kind != KindConnectionSaturation {
		t.Fatalf("expected only connection_saturation active, got %+v", active)
	}
}
