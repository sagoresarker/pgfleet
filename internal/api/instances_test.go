package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

type fakeInstanceStore struct {
	mu      sync.Mutex
	items   map[string]instance.Instance
	seq     int
	created []instance.NewInstance
}

func newFakeInstanceStore() *fakeInstanceStore {
	return &fakeInstanceStore{items: map[string]instance.Instance{}}
}

func (f *fakeInstanceStore) Create(_ context.Context, in instance.NewInstance) (instance.Instance, error) {
	if err := in.Validate(); err != nil {
		return instance.Instance{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := "inst-" + string(rune('0'+f.seq))
	inst := instance.Instance{ID: id, Name: in.Name, Status: instance.StatusProvisioning, RepoType: in.RepoType, Stanza: in.Name}
	f.items[id] = inst
	f.created = append(f.created, in)
	return inst, nil
}

func (f *fakeInstanceStore) Get(_ context.Context, id string) (instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.items[id]
	if !ok {
		return instance.Instance{}, apperr.New(apperr.KindNotFound, "not found")
	}
	return inst, nil
}

func (f *fakeInstanceStore) List(_ context.Context) ([]instance.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []instance.Instance
	for _, i := range f.items {
		out = append(out, i)
	}
	return out, nil
}

type fakeProvisioner struct {
	mu          sync.Mutex
	provisioned chan string
	visibility  chan string
	started     []string
	stopped     []string
	destroyed   []string
	marked      []string
	cloned      []string
	dsn         string
}

func newFakeProvisioner() *fakeProvisioner {
	return &fakeProvisioner{
		provisioned: make(chan string, 4),
		visibility:  make(chan string, 4),
		dsn:         "postgres://u:p@localhost:5432/postgres",
	}
}

func (f *fakeProvisioner) Provision(_ context.Context, id string, progress provision.ProgressFunc) error {
	if progress != nil {
		progress("ready", "ok")
	}
	f.provisioned <- id
	return nil
}
func (f *fakeProvisioner) Clone(_ context.Context, cloneID string, _ instance.Instance, _ provision.ProgressFunc) error {
	f.mu.Lock()
	f.cloned = append(f.cloned, cloneID)
	f.mu.Unlock()
	f.provisioned <- cloneID
	return nil
}
func (f *fakeProvisioner) Start(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}
func (f *fakeProvisioner) Stop(_ context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	return nil
}
func (f *fakeProvisioner) Restart(_ context.Context, _ string) error { return nil }
func (f *fakeProvisioner) SetVisibility(_ context.Context, id string, _ bool) error {
	f.visibility <- id
	return nil
}
func (f *fakeProvisioner) Destroy(_ context.Context, id string, _ bool) error {
	f.destroyed = append(f.destroyed, id)
	return nil
}
func (f *fakeProvisioner) DSN(_ context.Context, _ string) (string, error) { return f.dsn, nil }
func (f *fakeProvisioner) MarkError(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked = append(f.marked, id+": "+reason)
	return nil
}

func mountInstances(store InstanceStore, prov InstanceProvisioner) http.Handler {
	return mountInstancesHandler(NewInstancesHandler(store, prov, nil))
}

func mountInstancesHandler(h *InstancesHandler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/instances", h.Create)
	r.Get("/api/v1/instances", h.List)
	r.Get("/api/v1/instances/{id}", h.Get)
	r.Post("/api/v1/instances/{id}/start", h.Start)
	r.Post("/api/v1/instances/{id}/stop", h.Stop)
	r.Delete("/api/v1/instances/{id}", h.Destroy)
	r.Get("/api/v1/instances/{id}/connection", h.Connection)
	r.Post("/api/v1/instances/{id}/visibility", h.Visibility)
	r.Post("/api/v1/instances/{id}/clone", h.Clone)
	return h2(r)
}

// fakeCloneBackup records the instance backed up and can be made to fail, to
// exercise the clone auto-backup path.
type fakeCloneBackup struct {
	mu     sync.Mutex
	calls  []string
	failed chan struct{}
	fail   bool
}

func newFakeCloneBackup(fail bool) *fakeCloneBackup {
	return &fakeCloneBackup{fail: fail, failed: make(chan struct{}, 1)}
}

func (f *fakeCloneBackup) Run(_ context.Context, instanceID, backupType string) error {
	f.mu.Lock()
	f.calls = append(f.calls, instanceID+":"+backupType)
	f.mu.Unlock()
	if f.fail {
		select {
		case f.failed <- struct{}{}:
		default:
		}
		return apperr.New(apperr.KindInternal, "backup boom")
	}
	return nil
}

func (f *fakeCloneBackup) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// setInstance overwrites an instance row in the fake store (for role/cluster).
func (f *fakeInstanceStore) setInstance(inst instance.Instance) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[inst.ID] = inst
}

func h2(h http.Handler) http.Handler { return h }

func TestCreateInstanceReturns202AndProvisions(t *testing.T) {
	store := newFakeInstanceStore()
	prov := newFakeProvisioner()
	h := mountInstances(store, prov)

	rr := postJSON(t, h, "/api/v1/instances", `{"name":"orders-db","repo_type":"s3","password":"a-good-password"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rr.Code, rr.Body.String())
	}

	select {
	case <-prov.provisioned:
	case <-time.After(2 * time.Second):
		t.Fatal("provisioning was not triggered")
	}
}

func TestCreateInstanceValidation(t *testing.T) {
	h := mountInstances(newFakeInstanceStore(), newFakeProvisioner())
	for _, body := range []string{
		`{"name":"Bad_Name","repo_type":"s3","password":"a-good-password"}`,
		`{"name":"ok","repo_type":"nfs","password":"a-good-password"}`,
		`{"name":"ok","repo_type":"s3","password":"short"}`,
		`not json`,
	} {
		rr := postJSON(t, h, "/api/v1/instances", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", body, rr.Code)
		}
	}
}

func TestListAndGetInstance(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "a-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	h := mountInstances(store, newFakeProvisioner())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	var list struct {
		Instances []map[string]any `json:"instances"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Instances) != 1 {
		t.Errorf("list len = %d, want 1", len(list.Instances))
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status = %d", rr.Code)
	}
}

func TestGetMissingInstanceIs404(t *testing.T) {
	h := mountInstances(newFakeInstanceStore(), newFakeProvisioner())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/ghost", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestStartStopDestroyInstance(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "a-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	prov := newFakeProvisioner()
	h := mountInstances(store, prov)

	if rr := postJSON(t, h, "/api/v1/instances/"+inst.ID+"/start", ``); rr.Code != http.StatusNoContent {
		t.Errorf("start status = %d, want 204", rr.Code)
	}
	if rr := postJSON(t, h, "/api/v1/instances/"+inst.ID+"/stop", ``); rr.Code != http.StatusNoContent {
		t.Errorf("stop status = %d, want 204", rr.Code)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("destroy status = %d, want 204", rr.Code)
	}
	if len(prov.started) != 1 || len(prov.stopped) != 1 || len(prov.destroyed) != 1 {
		t.Errorf("lifecycle calls: start=%v stop=%v destroy=%v", prov.started, prov.stopped, prov.destroyed)
	}
}

// REG-5: the visibility handler must Get the instance first and 404 on a
// missing id, rather than blindly returning 202 and kicking off a flip on a
// nonexistent instance.
func TestVisibilityMissingInstanceIs404(t *testing.T) {
	prov := newFakeProvisioner()
	h := mountInstances(newFakeInstanceStore(), prov)

	rr := postJSON(t, h, "/api/v1/instances/ghost/visibility", `{"public":true}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a missing instance", rr.Code)
	}
	select {
	case id := <-prov.visibility:
		t.Fatalf("SetVisibility was invoked (%q) for a missing instance", id)
	case <-time.After(200 * time.Millisecond):
		// good: no flip triggered
	}
}

// A valid instance still gets a 202 and triggers the flip.
func TestVisibilityValidInstanceReturns202(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "a-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	prov := newFakeProvisioner()
	h := mountInstances(store, prov)

	rr := postJSON(t, h, "/api/v1/instances/"+inst.ID+"/visibility", `{"public":true}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case <-prov.visibility:
	case <-time.After(2 * time.Second):
		t.Fatal("SetVisibility was not triggered")
	}
}

// Clone auto-backup: a fresh full backup of the SOURCE is taken before the
// provisioner Clone runs, so the clone reflects the source's current state.
func TestCloneTakesSourceBackupBeforeCloning(t *testing.T) {
	store := newFakeInstanceStore()
	source, _ := store.Create(context.Background(), instance.NewInstance{Name: "src-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	prov := newFakeProvisioner()
	bak := newFakeCloneBackup(false)
	h := mountInstancesHandler(NewInstancesHandler(store, prov, nil).WithCloneBackup(bak))

	rr := postJSON(t, h, "/api/v1/instances/"+source.ID+"/clone", `{"name":"clone-db","password":"a-good-password"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rr.Code, rr.Body.String())
	}

	select {
	case <-prov.provisioned:
	case <-time.After(2 * time.Second):
		t.Fatal("clone was not triggered")
	}
	if bak.callCount() != 1 {
		t.Fatalf("source backup calls = %d, want 1", bak.callCount())
	}
	if got := bak.calls[0]; got != source.ID+":full" {
		t.Errorf("backup target = %q, want %q", got, source.ID+":full")
	}
	prov.mu.Lock()
	cloned := len(prov.cloned)
	prov.mu.Unlock()
	if cloned != 1 {
		t.Fatalf("clone was not executed after backup (cloned=%d)", cloned)
	}
}

// Clone auto-backup failure aborts the clone: the provisioner Clone never runs
// and the target is marked errored (no half-built target).
func TestCloneAbortsWhenSourceBackupFails(t *testing.T) {
	store := newFakeInstanceStore()
	source, _ := store.Create(context.Background(), instance.NewInstance{Name: "src-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	prov := newFakeProvisioner()
	bak := newFakeCloneBackup(true)
	h := mountInstancesHandler(NewInstancesHandler(store, prov, nil).WithCloneBackup(bak))

	rr := postJSON(t, h, "/api/v1/instances/"+source.ID+"/clone", `{"name":"clone-db","password":"a-good-password"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rr.Code, rr.Body.String())
	}

	select {
	case <-bak.failed:
	case <-time.After(2 * time.Second):
		t.Fatal("source backup was not attempted")
	}
	// Give the goroutine a moment to (not) call Clone and to MarkError.
	select {
	case id := <-prov.provisioned:
		t.Fatalf("Clone ran (%q) despite the source backup failing", id)
	case <-time.After(300 * time.Millisecond):
	}
	prov.mu.Lock()
	marked := append([]string(nil), prov.marked...)
	cloned := len(prov.cloned)
	prov.mu.Unlock()
	if cloned != 0 {
		t.Fatalf("Clone executed despite backup failure (cloned=%d)", cloned)
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "source backup failed") {
		t.Fatalf("target was not marked errored: %v", marked)
	}
}

// Destroy guard: a cluster PRIMARY cannot be destroyed directly (409 Conflict).
func TestDestroyClusterPrimaryIsRefused(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "prim-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	inst.Role = instance.RolePrimary
	inst.ClusterID = "clu-1"
	store.setInstance(inst)
	prov := newFakeProvisioner()
	h := mountInstances(store, prov)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "destroy the cluster") {
		t.Errorf("error body missing guidance: %s", rr.Body.String())
	}
	if len(prov.destroyed) != 0 {
		t.Errorf("Destroy was called for a cluster primary: %v", prov.destroyed)
	}
}

// Destroy guard: a cluster REPLICA may be destroyed directly.
// N5: directly destroying a cluster REPLICA is also refused — that path skips
// clusterctl and would leak the replica's replication slot on the primary.
func TestDestroyClusterReplicaIsRefused(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "repl-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	inst.Role = instance.RoleReplica
	inst.ClusterID = "clu-1"
	store.setInstance(inst)
	prov := newFakeProvisioner()
	h := mountInstances(store, prov)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rr.Code, rr.Body.String())
	}
	if len(prov.destroyed) != 0 {
		t.Errorf("clustered replica must not be destroyed directly: %v", prov.destroyed)
	}
}

// Destroy guard: a standalone instance is unaffected.
func TestDestroyStandaloneIsAllowed(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "solo-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	// Default role is standalone; no cluster.
	prov := newFakeProvisioner()
	h := mountInstances(store, prov)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rr.Code, rr.Body.String())
	}
	if len(prov.destroyed) != 1 {
		t.Errorf("standalone was not destroyed: %v", prov.destroyed)
	}
}

func TestConnectionReturnsDSN(t *testing.T) {
	store := newFakeInstanceStore()
	inst, _ := store.Create(context.Background(), instance.NewInstance{Name: "a-db", RepoType: instance.RepoS3, Password: "a-good-password"})
	h := mountInstances(store, newFakeProvisioner())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/connection", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "postgres://") {
		t.Errorf("body missing DSN: %s", rr.Body.String())
	}
}
