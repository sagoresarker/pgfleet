package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/metrics"
)

// TestMetricsRangeRejectsInvertedWindow — since after until is a client error.
func TestMetricsRangeRejectsInvertedWindow(t *testing.T) {
	h := mountMetrics(&fakeMetricsStore{}, nil)
	rr := getReq(t, h,
		"/api/v1/instances/i1/metrics/connections?since=2030-01-01T00:00:00Z&until=2020-01-01T00:00:00Z")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("inverted window status = %d, want 400", rr.Code)
	}
}

// TestMetricsQueriesLimitEdges — limit of 0, negative, non-numeric, or overflow
// must fall back to the default (no error, no panic).
func TestMetricsQueriesLimitEdges(t *testing.T) {
	var gotLimit int
	insights := func(_ context.Context, _ string, limit int) ([]metrics.QueryStat, error) {
		gotLimit = limit
		return nil, nil
	}
	h := mountMetrics(&fakeMetricsStore{}, insights)
	for _, v := range []string{"0", "-5", "abc", "99999999999999999999"} {
		gotLimit = -1
		rr := getReq(t, h, "/api/v1/instances/i1/queries?limit="+v)
		if rr.Code != http.StatusOK {
			t.Errorf("limit=%q status = %d, want 200", v, rr.Code)
		}
		if gotLimit != 20 {
			t.Errorf("limit=%q used limit %d, want default 20", v, gotLimit)
		}
	}
}
