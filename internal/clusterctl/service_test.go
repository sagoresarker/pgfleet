package clusterctl

import (
	"context"
	"sync"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

type fakeClusters struct {
	mu      sync.Mutex
	items   map[string]cluster.Cluster
	seq     int
	primary map[string]string
	router  map[string]int
	status  map[string]cluster.Status
}

func newFakeClusters() *fakeClusters {
	return &fakeClusters{items: map[string]cluster.Cluster{}, primary: map[string]string{}, router: map[string]int{}, status: map[string]cluster.Status{}}
}
func (f *fakeClusters) Create(_ context.Context, in cluster.NewCluster) (cluster.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	c := cluster.Cluster{ID: "c" + string(rune('0'+f.seq)), Name: in.Name, Status: cluster.StatusProvisioning}
	f.items[c.ID] = c
	f.status[c.ID] = c.Status
	return c, nil
}
func (f *fakeClusters) Get(_ context.Context, id string) (cluster.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.items[id]
	if !ok {
		return cluster.Cluster{}, apperr.New(apperr.KindNotFound, "nf")
	}
	c.Status = f.status[id]
	return c, nil
}
func (f *fakeClusters) SetPrimary(_ context.Context, id, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.primary[id] = p
	return nil
}
func (f *fakeClusters) SetRouter(_ context.Context, id, _ string, port int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.router[id] = port
	return nil
}
func (f *fakeClusters) SetStatus(_ context.Context, id string, s cluster.Status, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = s
	return nil
}
func (f *fakeClusters) Delete(_ context.Context, _ string) error { return nil }

type fakeInstances struct {
	mu      sync.Mutex
	items   map[string]instance.Instance
	seq     int
	created []instance.NewInstance
}

func newFakeInstances() *fakeInstances {
	return &fakeInstances{items: map[string]instance.Instance{}}
}
func (f *fakeInstances) Create(_ context.Context, in instance.NewInstance) (instance.Instance, error) {
	if err := in.Validate(); err != nil {
		return instance.Instance{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	inst := instance.Instance{ID: "i" + string(rune('0'+f.seq)), Name: in.Name, ClusterID: in.ClusterID, Role: in.Role, Superuser: "postgres"}
	f.items[inst.ID] = inst
	f.created = append(f.created, in)
	return inst, nil
}
func (f *fakeInstances) Get(_ context.Context, id string) (instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.items[id], nil
}
func (f *fakeInstances) Password(context.Context, string) (string, error) { return "pw", nil }
func (f *fakeInstances) ListByCluster(_ context.Context, clusterID string) ([]instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var primary []instance.Instance
	var replicas []instance.Instance
	for _, i := range f.items {
		if i.ClusterID != clusterID {
			continue
		}
		if i.Role == instance.RolePrimary {
			primary = append(primary, i)
		} else {
			replicas = append(replicas, i)
		}
	}
	return append(primary, replicas...), nil
}

type fakeProv struct {
	provisioned  []string
	replicas     []string
	routerCalled bool
	destroyed    []string
}

func (f *fakeProv) Provision(_ context.Context, id string, _ provision.ProgressFunc) error {
	f.provisioned = append(f.provisioned, id)
	return nil
}
func (f *fakeProv) ProvisionReplica(_ context.Context, id string, _ instance.Instance, _ provision.ProgressFunc) error {
	f.replicas = append(f.replicas, id)
	return nil
}
func (f *fakeProv) StartRouter(_ context.Context, _ provision.RouterSpec, _ provision.ProgressFunc) (string, int, error) {
	f.routerCalled = true
	return "router-c", 6432, nil
}
func (f *fakeProv) Destroy(_ context.Context, id string, _ bool) error {
	f.destroyed = append(f.destroyed, id)
	return nil
}
func (f *fakeProv) DropReplicationSlot(context.Context, instance.Instance, string) error { return nil }

type fakeRouter struct{ removed []string }

func (f *fakeRouter) RemoveContainer(_ context.Context, id string, _ bool) error {
	f.removed = append(f.removed, id)
	return nil
}

func TestCreateValidation(t *testing.T) {
	svc := New(newFakeClusters(), newFakeInstances(), &fakeProv{}, &fakeRouter{}, instance.RepoLocal)
	cases := []Input{
		{Name: "Bad_Name", Replicas: 1, Password: "a-good-password"},
		{Name: "ok", Replicas: -1, Password: "a-good-password"},
		{Name: "ok", Replicas: 20, Password: "a-good-password"},
	}
	for _, in := range cases {
		if _, err := svc.Create(context.Background(), in); apperr.Kind(err) != apperr.KindInvalid {
			t.Errorf("Create(%+v) kind = %v, want Invalid", in, apperr.Kind(err))
		}
	}
}

func TestCreateMakesPrimaryAndReplicas(t *testing.T) {
	insts := newFakeInstances()
	svc := New(newFakeClusters(), insts, &fakeProv{}, &fakeRouter{}, instance.RepoLocal)

	_, err := svc.Create(context.Background(), Input{Name: "orders", Replicas: 2, Password: "a-good-password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(insts.created) != 3 {
		t.Fatalf("created %d instances, want 3 (1 primary + 2 replicas)", len(insts.created))
	}
	if insts.created[0].Role != instance.RolePrimary || insts.created[0].Name != "orders-p" {
		t.Errorf("primary = %+v", insts.created[0])
	}
	if insts.created[1].Role != instance.RoleReplica || insts.created[2].Name != "orders-r2" {
		t.Errorf("replicas = %+v", insts.created[1:])
	}
}

func TestProvisionSequencesPrimaryReplicasThenRouter(t *testing.T) {
	clusters := newFakeClusters()
	insts := newFakeInstances()
	prov := &fakeProv{}
	svc := New(clusters, insts, prov, &fakeRouter{}, instance.RepoLocal)

	c, _ := svc.Create(context.Background(), Input{Name: "orders", Replicas: 2, Password: "a-good-password"})
	if err := svc.Provision(context.Background(), c.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if len(prov.provisioned) != 1 {
		t.Errorf("primary provisioned %d times, want 1", len(prov.provisioned))
	}
	if len(prov.replicas) != 2 {
		t.Errorf("replicas provisioned %d, want 2", len(prov.replicas))
	}
	if !prov.routerCalled {
		t.Error("router was not started")
	}
	if clusters.status[c.ID] != cluster.StatusRunning {
		t.Errorf("status = %q, want running", clusters.status[c.ID])
	}
	if clusters.router[c.ID] != 6432 {
		t.Errorf("router port = %d, want 6432", clusters.router[c.ID])
	}
}

func TestDestroyRemovesRouterAndMembers(t *testing.T) {
	clusters := newFakeClusters()
	insts := newFakeInstances()
	prov := &fakeProv{}
	router := &fakeRouter{}
	svc := New(clusters, insts, prov, router, instance.RepoLocal)

	c, _ := svc.Create(context.Background(), Input{Name: "orders", Replicas: 1, Password: "a-good-password"})
	_ = clusters.SetRouter(context.Background(), c.ID, "router-c", 6432)
	clusters.items[c.ID] = cluster.Cluster{ID: c.ID, Name: "orders", RouterContainerID: "router-c"}

	if err := svc.Destroy(context.Background(), c.ID, false); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(router.removed) != 1 {
		t.Error("router container not removed")
	}
	if len(prov.destroyed) != 2 {
		t.Errorf("destroyed %d members, want 2", len(prov.destroyed))
	}
}
