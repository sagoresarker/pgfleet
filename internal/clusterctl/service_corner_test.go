package clusterctl

import (
	"context"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// TestCreateRejectsLongClusterNameWithoutOrphan — a cluster name that is valid
// on its own but overflows the instance-name limit once the "-p"/"-rN" suffix
// is appended must be rejected up front, BEFORE any row is written. Otherwise
// the cluster row (and maybe the primary) is orphaned with no rollback.
func TestCreateRejectsLongClusterNameWithoutOrphan(t *testing.T) {
	clusters := newFakeClusters()
	insts := newFakeInstances()
	svc := New(clusters, insts, &fakeProv{}, &fakeRouter{}, instance.RepoLocal)

	// 38-char name is valid for a cluster ([a-z][a-z0-9-]{1,38} => up to 39),
	// but "<name>-p" is 40 chars, over the instance-name limit.
	name := "a" + strings.Repeat("b", 37) // 38 chars
	if !(len(name) == 38) {
		t.Fatalf("test setup: name len = %d", len(name))
	}

	_, err := svc.Create(context.Background(), Input{Name: name, Replicas: 1, Password: "a-good-password"})
	if apperr.Kind(err) != apperr.KindInvalid {
		t.Fatalf("Create kind = %v, want Invalid", apperr.Kind(err))
	}
	if len(clusters.items) != 0 {
		t.Errorf("orphan cluster rows: %d, want 0", len(clusters.items))
	}
	if len(insts.created) != 0 {
		t.Errorf("orphan instance rows: %d, want 0", len(insts.created))
	}
}

// failingInstances fails Create for one specific member name, simulating a
// mid-create conflict (e.g. a standalone instance already owns that name).
type failingInstances struct {
	*fakeInstances
	failName string
}

func (f *failingInstances) Create(ctx context.Context, in instance.NewInstance) (instance.Instance, error) {
	if in.Name == f.failName {
		return instance.Instance{}, apperr.New(apperr.KindConflict, "name taken")
	}
	return f.fakeInstances.Create(ctx, in)
}

// TestCreatePartialFailureCleansUpCluster — if a member Create fails partway
// through, the already-created cluster row must be deleted (which cascades to
// any member rows via the FK), leaving no orphan.
func TestCreatePartialFailureCleansUpCluster(t *testing.T) {
	clusters := newFakeClusters()
	insts := &failingInstances{fakeInstances: newFakeInstances(), failName: "orders-r2"}
	svc := New(clusters, insts, &fakeProv{}, &fakeRouter{}, instance.RepoLocal)

	_, err := svc.Create(context.Background(), Input{Name: "orders", Replicas: 2, Password: "a-good-password"})
	if apperr.Kind(err) != apperr.KindConflict {
		t.Fatalf("Create kind = %v, want Conflict", apperr.Kind(err))
	}
	if len(clusters.deleted) != 1 {
		t.Errorf("cluster not cleaned up on partial failure: deleted=%v", clusters.deleted)
	}
	if len(clusters.items) != 0 {
		t.Errorf("orphan cluster rows: %d, want 0", len(clusters.items))
	}
}

// TestCreateReplicaBoundaries — 0 replicas (primary only) is valid; exactly 10
// is invalid (max is 9).
func TestCreateReplicaBoundaries(t *testing.T) {
	svc := New(newFakeClusters(), newFakeInstances(), &fakeProv{}, &fakeRouter{}, instance.RepoLocal)

	if _, err := svc.Create(context.Background(), Input{Name: "solo", Replicas: 0, Password: "a-good-password"}); err != nil {
		t.Errorf("0 replicas should be valid: %v", err)
	}
	if _, err := svc.Create(context.Background(), Input{Name: "toomany", Replicas: 10, Password: "a-good-password"}); apperr.Kind(err) != apperr.KindInvalid {
		t.Errorf("10 replicas should be Invalid, got %v", apperr.Kind(err))
	}
}

// TestConnectionDSNRejectsMissingPrimary — when no member is a primary (e.g.
// mid-failover), ConnectionDSN must not hand out a replica's credentials as if
// it were the primary.
func TestConnectionDSNRejectsMissingPrimary(t *testing.T) {
	clusters := newFakeClusters()
	insts := newFakeInstances()
	svc := New(clusters, insts, &fakeProv{}, &fakeRouter{}, instance.RepoLocal)

	c, _ := clusters.Create(context.Background(), cluster.NewCluster{Name: "orders"})
	// Give it a ready router port directly on the stored item.
	stored := clusters.items[c.ID]
	stored.RouterPort = 6432
	clusters.items[c.ID] = stored
	// A lone replica member, no primary.
	_, _ = insts.Create(context.Background(), instance.NewInstance{
		Name: "orders-r1", RepoType: instance.RepoLocal, Password: "a-good-password",
		ClusterID: c.ID, Role: instance.RoleReplica,
	})

	if _, err := svc.ConnectionDSN(context.Background(), c.ID, "localhost"); err == nil {
		t.Error("ConnectionDSN with no primary must return an error")
	}
}
