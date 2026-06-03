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
	"github.com/sagoresarker/pgfleet/internal/pgcat"
)

type fakeAdminResolver struct {
	dsn string
	err error
}

func (f fakeAdminResolver) RouterAdminDSN(context.Context, string, string) (string, error) {
	return f.dsn, f.err
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
