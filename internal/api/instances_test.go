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
	provisioned chan string
	visibility  chan string
	started     []string
	stopped     []string
	destroyed   []string
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

func mountInstances(store InstanceStore, prov InstanceProvisioner) http.Handler {
	h := NewInstancesHandler(store, prov, nil)
	r := chi.NewRouter()
	r.Post("/api/v1/instances", h.Create)
	r.Get("/api/v1/instances", h.List)
	r.Get("/api/v1/instances/{id}", h.Get)
	r.Post("/api/v1/instances/{id}/start", h.Start)
	r.Post("/api/v1/instances/{id}/stop", h.Stop)
	r.Delete("/api/v1/instances/{id}", h.Destroy)
	r.Get("/api/v1/instances/{id}/connection", h.Connection)
	r.Post("/api/v1/instances/{id}/visibility", h.Visibility)
	return h2(r)
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
