package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/metrics"
)

type fakeMetricsStore struct {
	latest  map[string]metrics.Sample
	samples []metrics.Sample
}

func (f *fakeMetricsStore) Latest(context.Context, string) (map[string]metrics.Sample, error) {
	return f.latest, nil
}
func (f *fakeMetricsStore) Query(context.Context, string, string, time.Time, time.Time) ([]metrics.Sample, error) {
	return f.samples, nil
}

func mountMetrics(store MetricsStore, insights QueryInsightsFunc) http.Handler {
	h := NewMetricsHandler(store, insights)
	r := chi.NewRouter()
	r.Get("/api/v1/instances/{id}/metrics", h.Latest)
	r.Get("/api/v1/instances/{id}/metrics/{metric}", h.Range)
	r.Get("/api/v1/instances/{id}/queries", h.Queries)
	return r
}

func getReq(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestMetricsLatest(t *testing.T) {
	store := &fakeMetricsStore{latest: map[string]metrics.Sample{
		"connections": {Metric: "connections", Value: 5, At: time.Now()},
	}}
	h := mountMetrics(store, nil)

	rr := getReq(t, h, "/api/v1/instances/i1/metrics")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Metrics map[string]metrics.Sample `json:"metrics"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Metrics["connections"].Value != 5 {
		t.Errorf("connections = %v", resp.Metrics["connections"].Value)
	}
}

func TestMetricsRange(t *testing.T) {
	store := &fakeMetricsStore{samples: []metrics.Sample{
		{Metric: "connections", Value: 1, At: time.Now().Add(-time.Minute)},
		{Metric: "connections", Value: 2, At: time.Now()},
	}}
	h := mountMetrics(store, nil)

	rr := getReq(t, h, "/api/v1/instances/i1/metrics/connections?since=2026-06-03T00:00:00Z")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Samples []metrics.Sample `json:"samples"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Samples) != 2 {
		t.Errorf("samples = %d, want 2", len(resp.Samples))
	}
}

func TestMetricsRangeRejectsBadTime(t *testing.T) {
	h := mountMetrics(&fakeMetricsStore{}, nil)
	rr := getReq(t, h, "/api/v1/instances/i1/metrics/connections?since=not-a-time")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestMetricsQueries(t *testing.T) {
	insights := func(_ context.Context, _ string, _ int) ([]metrics.QueryStat, error) {
		return []metrics.QueryStat{{Query: "SELECT 1", Calls: 10, TotalTimeMS: 5}}, nil
	}
	h := mountMetrics(&fakeMetricsStore{}, insights)

	rr := getReq(t, h, "/api/v1/instances/i1/queries")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Queries []metrics.QueryStat `json:"queries"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Queries) != 1 || resp.Queries[0].Calls != 10 {
		t.Errorf("queries = %v", resp.Queries)
	}
}

func TestMetricsQueriesWithoutInsightsIsEmpty(t *testing.T) {
	h := mountMetrics(&fakeMetricsStore{}, nil)
	rr := getReq(t, h, "/api/v1/instances/i1/queries")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}
