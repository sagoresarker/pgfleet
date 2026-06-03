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

	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

type fakeClusterSvc struct {
	mu          sync.Mutex
	provisioned chan string
	destroyed   []string
}

func newFakeClusterSvc() *fakeClusterSvc {
	return &fakeClusterSvc{provisioned: make(chan string, 4)}
}

func (f *fakeClusterSvc) Create(_ context.Context, in clusterctl.Input) (cluster.Cluster, error) {
	return cluster.Cluster{ID: "c1", Name: in.Name, Status: cluster.StatusProvisioning}, nil
}
func (f *fakeClusterSvc) Provision(_ context.Context, id string, _ provision.ProgressFunc) error {
	f.provisioned <- id
	return nil
}
func (f *fakeClusterSvc) Destroy(_ context.Context, id string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, id)
	return nil
}
func (f *fakeClusterSvc) ConnectionDSN(_ context.Context, _, _ string) (string, error) {
	return "postgres://postgres:pw@localhost:6432/postgres", nil
}

type fakeClusterStore struct{ items []cluster.Cluster }

func (f fakeClusterStore) List(context.Context) ([]cluster.Cluster, error) { return f.items, nil }
func (f fakeClusterStore) Get(_ context.Context, id string) (cluster.Cluster, error) {
	for _, c := range f.items {
		if c.ID == id {
			return c, nil
		}
	}
	return cluster.Cluster{}, nil
}

type fakeMemberLister struct{ members []instance.Instance }

func (f fakeMemberLister) ListByCluster(context.Context, string) ([]instance.Instance, error) {
	return f.members, nil
}

func mountClusters(svc ClusterService, store ClusterStore, members MemberLister) http.Handler {
	h := NewClustersHandler(svc, store, members, "localhost", nil)
	r := chi.NewRouter()
	r.Post("/api/v1/clusters", h.Create)
	r.Get("/api/v1/clusters", h.List)
	r.Get("/api/v1/clusters/{id}", h.Get)
	r.Delete("/api/v1/clusters/{id}", h.Destroy)
	r.Get("/api/v1/clusters/{id}/connection", h.Connection)
	return r
}

func TestCreateClusterReturns202AndProvisions(t *testing.T) {
	svc := newFakeClusterSvc()
	h := mountClusters(svc, fakeClusterStore{}, fakeMemberLister{})

	rr := postJSON(t, h, "/api/v1/clusters", `{"name":"orders","replicas":2,"password":"a-good-password"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rr.Code, rr.Body.String())
	}
	select {
	case <-svc.provisioned:
	case <-time.After(2 * time.Second):
		t.Fatal("cluster provisioning not triggered")
	}
}

func TestCreateClusterRejectsBadInput(t *testing.T) {
	h := mountClusters(newFakeClusterSvc(), fakeClusterStore{}, fakeMemberLister{})
	for _, b := range []string{`{"name":"orders"}`, `not json`, `{"name":"orders","replicas":1}`} {
		rr := postJSON(t, h, "/api/v1/clusters", b)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", b, rr.Code)
		}
	}
}

func TestListAndGetCluster(t *testing.T) {
	store := fakeClusterStore{items: []cluster.Cluster{{ID: "c1", Name: "orders", Status: cluster.StatusRunning, RouterPort: 6432}}}
	members := fakeMemberLister{members: []instance.Instance{
		{ID: "i1", Name: "orders-p", Role: instance.RolePrimary},
		{ID: "i2", Name: "orders-r1", Role: instance.RoleReplica},
	}}
	h := mountClusters(newFakeClusterSvc(), store, members)

	rr := getReq(t, h, "/api/v1/clusters")
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	var list struct {
		Clusters []map[string]any `json:"clusters"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Clusters) != 1 {
		t.Errorf("clusters = %d, want 1", len(list.Clusters))
	}

	rr = getReq(t, h, "/api/v1/clusters/c1")
	if rr.Code != http.StatusOK {
		t.Fatalf("get status = %d", rr.Code)
	}
	var detail struct {
		Members []map[string]any `json:"members"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &detail)
	if len(detail.Members) != 2 {
		t.Errorf("members = %d, want 2", len(detail.Members))
	}
}

func TestDestroyCluster(t *testing.T) {
	svc := newFakeClusterSvc()
	h := mountClusters(svc, fakeClusterStore{items: []cluster.Cluster{{ID: "c1"}}}, fakeMemberLister{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/c1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("destroy status = %d, want 204", rr.Code)
	}
	if len(svc.destroyed) != 1 {
		t.Error("cluster not destroyed")
	}
}

func TestClusterConnection(t *testing.T) {
	h := mountClusters(newFakeClusterSvc(), fakeClusterStore{items: []cluster.Cluster{{ID: "c1"}}}, fakeMemberLister{})
	rr := getReq(t, h, "/api/v1/clusters/c1/connection")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "postgres://") {
		t.Errorf("body missing DSN: %s", rr.Body.String())
	}
}
