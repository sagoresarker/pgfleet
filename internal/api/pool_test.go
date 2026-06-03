package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/pgcat"
)

type fakeAdminResolver struct {
	dsn      string
	err      error
	backends []clusterctl.RouterBackend
}

func (f fakeAdminResolver) RouterAdminDSN(context.Context, string, string) (string, error) {
	return f.dsn, f.err
}

func (f fakeAdminResolver) RouterBackends(context.Context, string) ([]clusterctl.RouterBackend, error) {
	return f.backends, nil
}

func mountPool(reader PoolStatsReader, resolver RouterAdminResolver) http.Handler {
	h := NewPoolHandler(reader, resolver, "localhost")
	r := chi.NewRouter()
	r.Get("/api/v1/clusters/{id}/pool/stats", h.Stats)
	return r
}

func TestPoolStatsOK(t *testing.T) {
	var gotDSN string
	reader := PoolStatsReaderFunc(func(_ context.Context, dsn string) (pgcat.PoolStats, error) {
		gotDSN = dsn
		return pgcat.PoolStats{
			Pools: []pgcat.PoolStat{{Database: "postgres", User: "postgres", ClWaiting: 2, SvActive: 3}},
			Stats: []pgcat.Stat{{Database: "postgres", TotalQueryCount: 42, AvgQueryTime: 7}},
		}, nil
	})
	resolver := fakeAdminResolver{dsn: "postgres://admin:pw@localhost:6432/pgcat"}

	h := mountPool(reader, resolver)
	rr := getReq(t, h, "/api/v1/clusters/c1/pool/stats")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if gotDSN != resolver.dsn {
		t.Errorf("reader got DSN %q, want %q", gotDSN, resolver.dsn)
	}

	var resp struct {
		Pools []pgcat.PoolStat `json:"pools"`
		Stats []pgcat.Stat     `json:"stats"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Pools) != 1 || resp.Pools[0].ClWaiting != 2 || resp.Pools[0].SvActive != 3 {
		t.Errorf("pools = %+v", resp.Pools)
	}
	if len(resp.Stats) != 1 || resp.Stats[0].TotalQueryCount != 42 || resp.Stats[0].AvgQueryTime != 7 {
		t.Errorf("stats = %+v", resp.Stats)
	}
}

func TestBuildRoutingAttributesTrafficByRole(t *testing.T) {
	backends := []clusterctl.RouterBackend{
		{Name: "orders-p", Role: "primary", Address: "pgfleet-pg-orders-p"},
		{Name: "orders-r1", Role: "replica", Address: "pgfleet-pg-orders-r1"},
	}
	servers := []pgcat.ServerStat{
		{Address: "pgfleet-pg-orders-p:5432", State: "active", QueryCount: 10, BytesSent: 100},
		{Address: "pgfleet-pg-orders-r1:5432", State: "active", QueryCount: 40, BytesSent: 400},
		{Address: "pgfleet-pg-orders-r1:5432", State: "idle", QueryCount: 35, BytesSent: 350},
	}
	routing := buildRouting(backends, servers)
	if len(routing) != 2 {
		t.Fatalf("len = %d, want 2", len(routing))
	}
	pri, rep := routing[0], routing[1]
	if pri.Role != "primary" || pri.Connections != 1 || pri.ActiveConns != 1 || pri.QueryCount != 10 {
		t.Errorf("primary = %+v", pri)
	}
	// Replica must collect BOTH its rows (active + idle), not be stolen by the
	// primary whose address is a prefix of the replica's.
	if rep.Role != "replica" || rep.Connections != 2 || rep.ActiveConns != 1 || rep.QueryCount != 75 || rep.BytesSent != 750 {
		t.Errorf("replica = %+v", rep)
	}
}

func TestBuildRoutingKeepsBackendsWithNoTraffic(t *testing.T) {
	backends := []clusterctl.RouterBackend{{Name: "p", Role: "primary", Address: "pgfleet-pg-p"}}
	routing := buildRouting(backends, nil)
	if len(routing) != 1 || routing[0].Connections != 0 || routing[0].Name != "p" {
		t.Fatalf("routing = %+v", routing)
	}
}

func TestPoolStatsIncludesClientsServersRouting(t *testing.T) {
	reader := PoolStatsReaderFunc(func(context.Context, string) (pgcat.PoolStats, error) {
		return pgcat.PoolStats{
			Pools:   []pgcat.PoolStat{{Database: "postgres"}},
			Servers: []pgcat.ServerStat{{Address: "pgfleet-pg-c1-p:5432", State: "active", QueryCount: 9}},
			Clients: []pgcat.ClientStat{{Database: "postgres", User: "app", State: "active", QueryCount: 3}},
		}, nil
	})
	resolver := fakeAdminResolver{
		dsn:      "postgres://admin:pw@localhost:6432/pgcat",
		backends: []clusterctl.RouterBackend{{Name: "c1-p", Role: "primary", Address: "pgfleet-pg-c1-p"}},
	}

	h := mountPool(reader, resolver)
	rr := getReq(t, h, "/api/v1/clusters/c1/pool/stats")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Clients []pgcat.ClientStat `json:"clients"`
		Servers []pgcat.ServerStat `json:"servers"`
		Routing []RoutingBackend   `json:"routing"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Clients) != 1 || resp.Clients[0].User != "app" {
		t.Errorf("clients = %+v", resp.Clients)
	}
	if len(resp.Routing) != 1 || resp.Routing[0].Role != "primary" || resp.Routing[0].QueryCount != 9 {
		t.Errorf("routing = %+v", resp.Routing)
	}
}

func TestPoolStatsResolverNotFound(t *testing.T) {
	reader := PoolStatsReaderFunc(func(context.Context, string) (pgcat.PoolStats, error) {
		t.Fatal("reader should not be called when resolver fails")
		return pgcat.PoolStats{}, nil
	})
	resolver := fakeAdminResolver{err: apperr.New(apperr.KindNotFound, "cluster: router not ready")}

	h := mountPool(reader, resolver)
	rr := getReq(t, h, "/api/v1/clusters/missing/pool/stats")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestPoolStatsReaderError(t *testing.T) {
	reader := PoolStatsReaderFunc(func(context.Context, string) (pgcat.PoolStats, error) {
		return pgcat.PoolStats{}, errors.New("connection refused")
	})
	resolver := fakeAdminResolver{dsn: "postgres://admin:pw@localhost:6432/pgcat"}

	h := mountPool(reader, resolver)
	rr := getReq(t, h, "/api/v1/clusters/c1/pool/stats")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	// Internal errors must not leak the underlying message.
	if body := rr.Body.String(); !json.Valid([]byte(body)) || strings.Contains(body, "connection refused") {
		t.Errorf("leaked internal error: %s", body)
	}
}
