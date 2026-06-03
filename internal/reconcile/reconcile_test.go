package reconcile

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

type memStore struct {
	items   map[string]instance.Instance
	status  map[string]instance.Status
	runtime map[string]string
	ports   map[string]int
}

func newStore(items ...instance.Instance) *memStore {
	m := &memStore{items: map[string]instance.Instance{}, status: map[string]instance.Status{}, runtime: map[string]string{}, ports: map[string]int{}}
	for _, i := range items {
		m.items[i.ID] = i
		m.status[i.ID] = i.Status
	}
	return m
}

func (m *memStore) List(context.Context) ([]instance.Instance, error) {
	var out []instance.Instance
	for _, i := range m.items {
		i.Status = m.status[i.ID]
		out = append(out, i)
	}
	return out, nil
}
func (m *memStore) SetStatus(_ context.Context, id string, s instance.Status, _ string) error {
	m.status[id] = s
	return nil
}
func (m *memStore) SetRuntime(_ context.Context, id, cid string, port int) error {
	m.runtime[id] = cid
	m.ports[id] = port
	inst := m.items[id]
	inst.ContainerID = cid
	m.items[id] = inst
	return nil
}

// startContainerFor creates a fake container labeled for an instance.
func startContainerFor(t *testing.T, rt *docker.Fake, instID string, start bool) string {
	t.Helper()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{
		Name:   "pgfleet-pg-" + instID,
		Labels: map[string]string{docker.LabelManaged: "true", docker.LabelInstance: instID},
		Ports:  []docker.PortMapping{{ContainerPort: 5432}},
	})
	if start {
		_ = rt.StartContainer(context.Background(), id)
	}
	return id
}

func TestReconcileMarksMissingContainerAsError(t *testing.T) {
	rt := docker.NewFake()
	store := newStore(instance.Instance{ID: "i1", Name: "db", Status: instance.StatusRunning, ContainerID: "gone"})
	r := New(rt, store, nil)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.status["i1"] != instance.StatusError {
		t.Errorf("status = %q, want error (container missing)", store.status["i1"])
	}
}

func TestReconcileSyncsStoppedContainer(t *testing.T) {
	rt := docker.NewFake()
	cid := startContainerFor(t, rt, "i1", false) // created but not started => "created"/stopped
	store := newStore(instance.Instance{ID: "i1", Name: "db", Status: instance.StatusRunning, ContainerID: cid})
	r := New(rt, store, nil)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.status["i1"] != instance.StatusStopped {
		t.Errorf("status = %q, want stopped", store.status["i1"])
	}
}

func TestReconcileAdoptsRunningContainer(t *testing.T) {
	rt := docker.NewFake()
	cid := startContainerFor(t, rt, "i1", true)
	// DB knows the instance but lost the container id (control-plane restart).
	store := newStore(instance.Instance{ID: "i1", Name: "db", Status: instance.StatusStopped, ContainerID: ""})
	r := New(rt, store, nil)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.runtime["i1"] != cid {
		t.Errorf("container not adopted: runtime=%q want %q", store.runtime["i1"], cid)
	}
	if store.status["i1"] != instance.StatusRunning {
		t.Errorf("status = %q, want running", store.status["i1"])
	}
}

func TestReconcileLeavesErroredInstancesAlone(t *testing.T) {
	rt := docker.NewFake()
	store := newStore(instance.Instance{ID: "i1", Name: "db", Status: instance.StatusError, ContainerID: "gone"})
	r := New(rt, store, nil)

	_ = r.Reconcile(context.Background())
	if store.status["i1"] != instance.StatusError {
		t.Errorf("errored instance should be left as error, got %q", store.status["i1"])
	}
}
