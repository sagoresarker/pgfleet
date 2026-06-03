package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// fakeComposeInstances is a stub composeInstanceReader.
type fakeComposeInstances struct {
	inst instance.Instance
	err  error
}

func (f fakeComposeInstances) Get(context.Context, string) (instance.Instance, error) {
	return f.inst, f.err
}

// fakeComposeClusters is a stub composeClusterReader returning a cluster and its
// members (primary first), or an error.
type fakeComposeClusters struct {
	cl      cluster.Cluster
	members []instance.Instance
	err     error
}

func (f fakeComposeClusters) Get(context.Context, string) (cluster.Cluster, error) {
	return f.cl, f.err
}

func (f fakeComposeClusters) ListByCluster(context.Context, string) ([]instance.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.members, nil
}

// TestComposeInstanceReturnsYAML verifies the instance endpoint returns 200 with
// a YAML body and download headers.
func TestComposeInstanceReturnsYAML(t *testing.T) {
	h := NewComposeHandler(
		fakeComposeInstances{inst: instance.Instance{
			ID: "i1", Name: "shop", PGVersion: "16",
			Image: "pgfleet/postgres-pgbackrest:16", RepoType: instance.RepoLocal,
			Superuser: "postgres",
		}},
		fakeComposeClusters{},
	)

	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodGet, "/", nil), "i1")
	h.GetInstance(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	cd := rr.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="shop-compose.yml"`) {
		t.Errorf("Content-Disposition = %q, want shop-compose.yml attachment", cd)
	}
	body := rr.Body.String()
	for _, want := range []string{"services:", "image: pgfleet/postgres-pgbackrest:16", "${POSTGRES_PASSWORD}"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

// TestComposeInstanceNotFound maps a KindNotFound lookup error to 404.
func TestComposeInstanceNotFound(t *testing.T) {
	h := NewComposeHandler(
		fakeComposeInstances{err: apperr.New(apperr.KindNotFound, "instance: not found")},
		fakeComposeClusters{},
	)
	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodGet, "/", nil), "missing")
	h.GetInstance(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// TestComposeClusterReturnsYAML verifies the cluster endpoint returns 200 with a
// compose document containing the members and the router service.
func TestComposeClusterReturnsYAML(t *testing.T) {
	members := []instance.Instance{
		{ID: "p", Name: "hac-p", PGVersion: "16", Image: "pgfleet/postgres-pgbackrest:16",
			RepoType: instance.RepoLocal, Superuser: "postgres", Role: instance.RolePrimary},
		{ID: "r1", Name: "hac-r1", PGVersion: "16", Image: "pgfleet/postgres-pgbackrest:16",
			RepoType: instance.RepoLocal, Superuser: "postgres", Role: instance.RoleReplica},
	}
	h := NewComposeHandler(
		fakeComposeInstances{},
		fakeComposeClusters{cl: cluster.Cluster{ID: "c1", Name: "hac"}, members: members},
	)

	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodGet, "/", nil), "c1")
	h.GetCluster(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="hac-compose.yml"`) {
		t.Errorf("Content-Disposition = %q, want hac-compose.yml attachment", cd)
	}
	body := rr.Body.String()
	for _, want := range []string{"services:", "pgfleet-pg-hac-p:", "pgfleet-pg-hac-r1:", "pgfleet-router-hac:"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

// TestComposeClusterNotFound maps a KindNotFound cluster lookup error to 404.
func TestComposeClusterNotFound(t *testing.T) {
	h := NewComposeHandler(
		fakeComposeInstances{},
		fakeComposeClusters{err: apperr.New(apperr.KindNotFound, "cluster: not found")},
	)
	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodGet, "/", nil), "missing")
	h.GetCluster(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}
