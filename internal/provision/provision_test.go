package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// memStore is an in-memory store for unit-testing orchestration.
type memStore struct {
	inst       instance.Instance
	password   string
	status     instance.Status
	lastErr    string
	container  string
	hostPort   int
	dataVolume string
	deleted    bool
	passwordErr error // if set, Password returns this error
}

func (m *memStore) Get(context.Context, string) (instance.Instance, error) {
	in := m.inst
	in.Status = m.status
	in.ContainerID = m.container
	in.HostPort = m.hostPort
	in.DataVolume = m.dataVolume
	return in, nil
}
func (m *memStore) Password(context.Context, string) (string, error) {
	if m.passwordErr != nil {
		return "", m.passwordErr
	}
	return m.password, nil
}
func (m *memStore) SetRuntime(_ context.Context, _, cid string, port int) error {
	m.container, m.hostPort = cid, port
	return nil
}
func (m *memStore) SetDataVolume(_ context.Context, _, vol string) error {
	m.dataVolume = vol
	return nil
}
func (m *memStore) SetStatus(_ context.Context, _ string, s instance.Status, e string) error {
	m.status, m.lastErr = s, e
	return nil
}
func (m *memStore) Delete(context.Context, string) error { m.deleted = true; return nil }

func newStore() *memStore {
	return &memStore{
		inst: instance.Instance{
			ID: "inst-1", Name: "orders-db", Image: instance.DefaultImage,
			RepoType: instance.RepoLocal, Stanza: "orders-db", Superuser: "postgres",
		},
		password: "pw",
		status:   instance.StatusProvisioning,
	}
}

func TestProvisionHappyPath(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(_ string, _ []string) (docker.ExecResult, error) {
		return docker.ExecResult{ExitCode: 0}, nil // pg_isready, config, stanza, check all succeed
	}
	store := newStore()
	p := New(rt, store, Options{Network: "pgfleet", InstanceHost: "localhost"})

	var steps []string
	err := p.Provision(context.Background(), "inst-1", func(step, _ string) {
		steps = append(steps, step)
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if store.status != instance.StatusRunning {
		t.Errorf("status = %q, want running", store.status)
	}
	if store.container == "" || store.hostPort == 0 {
		t.Errorf("runtime not recorded: container=%q port=%d", store.container, store.hostPort)
	}

	for _, want := range []string{"image", "container", "waiting", "stanza", "check", "ready"} {
		if !contains(steps, want) {
			t.Errorf("missing progress step %q in %v", want, steps)
		}
	}
}

func TestProvisionCreatesLabeledContainerAndVolumes(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(string, []string) (docker.ExecResult, error) { return docker.ExecResult{}, nil }
	store := newStore()
	p := New(rt, store, Options{Network: "pgfleet"})

	if err := p.Provision(context.Background(), "inst-1", nil); err != nil {
		t.Fatal(err)
	}

	got, _ := rt.ListByLabel(context.Background(), map[string]string{docker.LabelInstance: "inst-1"})
	if len(got) != 1 {
		t.Fatalf("expected 1 labeled container, got %d", len(got))
	}
	// data + repo (local) volumes both created and labeled.
	vols, _ := rt.ListVolumesByLabel(context.Background(), map[string]string{docker.LabelInstance: "inst-1"})
	if len(vols) != 2 {
		t.Errorf("expected 2 volumes (data+repo) for local repo, got %v", vols)
	}
}

func TestProvisionFailsWhenCheckFails(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "check" {
			return docker.ExecResult{ExitCode: 1, Stderr: "archive failed"}, nil
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	store := newStore()
	p := New(rt, store, Options{})

	err := p.Provision(context.Background(), "inst-1", nil)
	if err == nil {
		t.Fatal("expected provision to fail when check fails")
	}
	if store.status != instance.StatusError {
		t.Errorf("status = %q, want error", store.status)
	}
	if !strings.Contains(store.lastErr, "archive failed") {
		t.Errorf("last error = %q, want it to mention the check failure", store.lastErr)
	}
}

func TestDSN(t *testing.T) {
	rt := docker.NewFake()
	store := newStore()
	store.container, store.hostPort, store.status = "c1", 54321, instance.StatusRunning
	p := New(rt, store, Options{InstanceHost: "db.example.com"})

	dsn, err := p.DSN(context.Background(), "inst-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "postgres://postgres:pw@db.example.com:54321/postgres?sslmode=disable"
	if dsn != want {
		t.Errorf("DSN = %q, want %q", dsn, want)
	}
}

func TestDestroyLocalRemovesRepoVolumeUnlessRetained(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(string, []string) (docker.ExecResult, error) { return docker.ExecResult{}, nil }
	store := newStore()
	p := New(rt, store, Options{})
	_ = p.Provision(context.Background(), "inst-1", nil)

	if err := p.Destroy(context.Background(), "inst-1", false); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !store.deleted {
		t.Error("instance row should be deleted")
	}
	vols, _ := rt.ListVolumesByLabel(context.Background(), map[string]string{docker.LabelInstance: "inst-1"})
	if len(vols) != 0 {
		t.Errorf("volumes should be removed, got %v", vols)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}
