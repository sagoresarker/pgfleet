package failover

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// fakeProm scripts reachability + LSNs and records actions.
type fakeProm struct {
	reachable map[string]bool  // instanceID -> reachable
	lsn       map[string]int64 // replicaID -> replay lsn
	promoted  []string
	fenced    []string
	prepared  []string
	recloned  []string
	router    bool
	fenceErr  error
}

func (f *fakeProm) PrimaryReachable(_ context.Context, inst instance.Instance) bool {
	return f.reachable[inst.ID]
}
func (f *fakeProm) ReplayLSN(_ context.Context, inst instance.Instance) (int64, error) {
	return f.lsn[inst.ID], nil
}
func (f *fakeProm) Promote(_ context.Context, inst instance.Instance) error {
	f.promoted = append(f.promoted, inst.ID)
	return nil
}
func (f *fakeProm) Fence(_ context.Context, inst instance.Instance) error {
	if f.fenceErr != nil {
		return f.fenceErr
	}
	f.fenced = append(f.fenced, inst.ID)
	return nil
}
func (f *fakeProm) PrepareReclone(_ context.Context, inst instance.Instance) error {
	f.prepared = append(f.prepared, inst.ID)
	return nil
}
func (f *fakeProm) ProvisionReplica(_ context.Context, replicaID string, _ instance.Instance, _ provision.ProgressFunc) error {
	f.recloned = append(f.recloned, replicaID)
	return nil
}
func (f *fakeProm) StartRouter(_ context.Context, _ provision.RouterSpec, _ provision.ProgressFunc) (string, int, error) {
	f.router = true
	return "router-new", 6432, nil
}

type fakeClusters struct {
	items   map[string]cluster.Cluster
	primary map[string]string
	status  map[string]cluster.Status
}

func (f *fakeClusters) List(context.Context) ([]cluster.Cluster, error) {
	out := make([]cluster.Cluster, 0, len(f.items))
	for _, c := range f.items {
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeClusters) SetPrimary(_ context.Context, id, p string) error {
	f.primary[id] = p
	return nil
}
func (f *fakeClusters) SetRouter(context.Context, string, string, int) error { return nil }
func (f *fakeClusters) SetStatus(_ context.Context, id string, s cluster.Status, _ string) error {
	f.status[id] = s
	return nil
}

type fakeInstances struct {
	byCluster map[string][]instance.Instance
	items     map[string]instance.Instance
	roles     map[string]instance.Role
}

func (f *fakeInstances) ListByCluster(_ context.Context, id string) ([]instance.Instance, error) {
	return f.byCluster[id], nil
}
func (f *fakeInstances) Get(_ context.Context, id string) (instance.Instance, error) {
	return f.items[id], nil
}
func (f *fakeInstances) Password(context.Context, string) (string, error) { return "pw", nil }
func (f *fakeInstances) SetRole(_ context.Context, id string, r instance.Role) error {
	f.roles[id] = r
	return nil
}
func (f *fakeInstances) SetStatus(context.Context, string, instance.Status, string) error { return nil }

type fakeRouter struct{ removed []string }

func (f *fakeRouter) RemoveContainer(_ context.Context, id string, _ bool) error {
	f.removed = append(f.removed, id)
	return nil
}

func setup() (*fakeProm, *fakeClusters, *fakeInstances, *fakeRouter, cluster.Cluster) {
	clu := cluster.Cluster{ID: "c1", Name: "orders", Status: cluster.StatusRunning, RouterContainerID: "router-old"}
	primary := instance.Instance{ID: "p", Name: "orders-p", Role: instance.RolePrimary, ContainerID: "cp", Superuser: "postgres"}
	r1 := instance.Instance{ID: "r1", Name: "orders-r1", Role: instance.RoleReplica, ContainerID: "cr1", Superuser: "postgres"}
	r2 := instance.Instance{ID: "r2", Name: "orders-r2", Role: instance.RoleReplica, ContainerID: "cr2", Superuser: "postgres"}

	prom := &fakeProm{
		reachable: map[string]bool{"p": false, "r1": true, "r2": true}, // primary dead
		lsn:       map[string]int64{"r1": 100, "r2": 200},              // r2 more caught-up
	}
	clusters := &fakeClusters{
		items:   map[string]cluster.Cluster{"c1": clu},
		primary: map[string]string{}, status: map[string]cluster.Status{},
	}
	insts := &fakeInstances{
		byCluster: map[string][]instance.Instance{"c1": {primary, r1, r2}},
		items:     map[string]instance.Instance{"p": primary, "r1": r1, "r2": r2},
		roles:     map[string]instance.Role{},
	}
	return prom, clusters, insts, &fakeRouter{}, clu
}

// TestFailoverWaitsForThreshold — a single failed check must NOT trigger a
// failover (avoids reacting to transient blips).
func TestFailoverWaitsForThreshold(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	c := New(clusters, insts, prom, router, nil, 3, nil)

	_ = c.Run(context.Background()) // strike 1
	_ = c.Run(context.Background()) // strike 2
	if len(prom.promoted) != 0 {
		t.Fatalf("promoted before threshold: %v", prom.promoted)
	}
}

// TestFailoverPromotesMostCaughtUpReplica — after the threshold, the highest-LSN
// reachable replica is promoted, the old primary is fenced, and the router is
// repointed.
func TestFailoverPromotesMostCaughtUpReplica(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	c := New(clusters, insts, prom, router, nil, 3, nil)

	for range 3 {
		_ = c.Run(context.Background())
	}

	if len(prom.promoted) != 1 || prom.promoted[0] != "r2" {
		t.Errorf("promoted = %v, want [r2] (highest LSN)", prom.promoted)
	}
	if len(prom.fenced) != 1 || prom.fenced[0] != "p" {
		t.Errorf("old primary not fenced (stopped+removed): fenced = %v", prom.fenced)
	}
	if len(prom.prepared) != 1 || prom.prepared[0] != "r1" {
		t.Errorf("other replica not reset before reclone: prepared = %v", prom.prepared)
	}
	if clusters.primary["c1"] != "r2" {
		t.Errorf("cluster primary = %q, want r2", clusters.primary["c1"])
	}
	if insts.roles["r2"] != instance.RolePrimary {
		t.Errorf("r2 role = %q, want primary", insts.roles["r2"])
	}
	if insts.roles["p"] != instance.RoleReplica {
		t.Errorf("old primary role = %q, want demoted to replica", insts.roles["p"])
	}
	if len(prom.recloned) != 1 || prom.recloned[0] != "r1" {
		t.Errorf("other replica not reattached: recloned = %v", prom.recloned)
	}
	if !prom.router {
		t.Error("router was not repointed")
	}
	if len(router.removed) != 1 || router.removed[0] != "router-old" {
		t.Errorf("old router not removed: %v", router.removed)
	}
	if clusters.status["c1"] != cluster.StatusRunning {
		t.Errorf("cluster status = %q, want running", clusters.status["c1"])
	}
}

// TestFailoverAbortsWithoutReplica — a dead primary and no promotable replica
// must not promote anything; the cluster is marked error.
func TestFailoverAbortsWithoutReplica(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	// Both replicas also unreachable.
	prom.reachable["r1"] = false
	prom.reachable["r2"] = false
	c := New(clusters, insts, prom, router, nil, 1, nil)

	_ = c.Run(context.Background())
	if len(prom.promoted) != 0 {
		t.Errorf("promoted with no healthy replica: %v", prom.promoted)
	}
	if clusters.status["c1"] != cluster.StatusError {
		t.Errorf("status = %q, want error", clusters.status["c1"])
	}
}

// TestFailoverAbortsIfFenceFails — if the old primary cannot be fenced (we can't
// guarantee it is down), we must NOT promote a replica (split-brain risk).
func TestFailoverAbortsIfFenceFails(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	prom.fenceErr = errFence
	c := New(clusters, insts, prom, router, nil, 1, nil)

	_ = c.Run(context.Background())
	if len(prom.promoted) != 0 {
		t.Errorf("promoted despite a failed fence (split-brain risk): %v", prom.promoted)
	}
	if clusters.status["c1"] != cluster.StatusError {
		t.Errorf("status = %q, want error", clusters.status["c1"])
	}
}

// TestFailoverWontPromoteZeroProgressStandby — a replica that has replayed no
// WAL (lsn 0) is not promotable (would lose everything).
func TestFailoverWontPromoteZeroProgressStandby(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	prom.lsn["r1"] = 0
	prom.lsn["r2"] = 0
	c := New(clusters, insts, prom, router, nil, 1, nil)

	_ = c.Run(context.Background())
	if len(prom.promoted) != 0 {
		t.Errorf("promoted a zero-progress standby: %v", prom.promoted)
	}
}

// TestFailoverQuorumMetPromotes — 3-node cluster (1 primary + 2 replicas,
// majority = 2). The primary is down but BOTH replicas are reachable, so the
// controller is on the majority side of any partition and may safely promote.
func TestFailoverQuorumMetPromotes(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	// reachable: r1+r2 (2 of 3 members) => strict majority => quorum met.
	c := New(clusters, insts, prom, router, nil, 1, nil)

	_ = c.Run(context.Background())
	if len(prom.promoted) != 1 || prom.promoted[0] != "r2" {
		t.Errorf("quorum met but did not promote highest-LSN replica: promoted = %v", prom.promoted)
	}
	if clusters.status["c1"] != cluster.StatusRunning {
		t.Errorf("status = %q, want running", clusters.status["c1"])
	}
}

// TestFailoverQuorumNotMetAborts — 3-node cluster (majority = 2). The primary is
// down and only ONE replica is reachable (the other replica + the primary are on
// the far side of a partition). The controller is likely on the MINORITY side,
// so promoting risks split-brain. It must NOT promote and must mark the cluster
// degraded/error with a quorum message.
func TestFailoverQuorumNotMetAborts(t *testing.T) {
	prom, clusters, insts, router, _ := setup()
	// Only r1 reachable; r2 (and the primary) are partitioned away.
	prom.reachable["r2"] = false
	c := New(clusters, insts, prom, router, nil, 1, nil)

	_ = c.Run(context.Background())
	if len(prom.promoted) != 0 {
		t.Errorf("promoted without quorum (split-brain risk): %v", prom.promoted)
	}
	if len(prom.fenced) != 0 {
		t.Errorf("fenced old primary without quorum: %v", prom.fenced)
	}
	if clusters.status["c1"] != cluster.StatusError && clusters.status["c1"] != cluster.StatusDegraded {
		t.Errorf("status = %q, want error/degraded (quorum not met)", clusters.status["c1"])
	}
}

// TestFailoverSingleReplicaQuorumPromotes — a 2-node cluster (1 primary + 1
// replica, total = 2, majority = 2). The primary is down and the one replica is
// reachable. That single replica is the only candidate, so the math must not
// deadlock legitimate 2-node failover: it still promotes. (2-node clusters
// cannot truly avoid split-brain without a witness — see the comment in
// failover.go.)
func TestFailoverSingleReplicaQuorumPromotes(t *testing.T) {
	clu := cluster.Cluster{ID: "c1", Name: "orders", Status: cluster.StatusRunning, RouterContainerID: "router-old"}
	primary := instance.Instance{ID: "p", Name: "orders-p", Role: instance.RolePrimary, ContainerID: "cp", Superuser: "postgres"}
	r1 := instance.Instance{ID: "r1", Name: "orders-r1", Role: instance.RoleReplica, ContainerID: "cr1", Superuser: "postgres"}

	prom := &fakeProm{
		reachable: map[string]bool{"p": false, "r1": true},
		lsn:       map[string]int64{"r1": 100},
	}
	clusters := &fakeClusters{
		items:   map[string]cluster.Cluster{"c1": clu},
		primary: map[string]string{}, status: map[string]cluster.Status{},
	}
	insts := &fakeInstances{
		byCluster: map[string][]instance.Instance{"c1": {primary, r1}},
		items:     map[string]instance.Instance{"p": primary, "r1": r1},
		roles:     map[string]instance.Role{},
	}
	c := New(clusters, insts, prom, &fakeRouter{}, nil, 1, nil)

	_ = c.Run(context.Background())
	if len(prom.promoted) != 1 || prom.promoted[0] != "r1" {
		t.Errorf("2-node failover did not promote the sole replica: promoted = %v", prom.promoted)
	}
	if clusters.status["c1"] != cluster.StatusRunning {
		t.Errorf("status = %q, want running", clusters.status["c1"])
	}
}

var errFence = &fenceError{}

type fenceError struct{}

func (*fenceError) Error() string { return "cannot reach docker daemon" }
