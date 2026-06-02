package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/metrics"
)

// MetricsStore reads stored metric samples.
type MetricsStore interface {
	Latest(ctx context.Context, instanceID string) (map[string]metrics.Sample, error)
	Query(ctx context.Context, instanceID, metric string, since, until time.Time) ([]metrics.Sample, error)
}

// QueryInsightsFunc returns top queries for an instance (may be nil).
type QueryInsightsFunc func(ctx context.Context, instanceID string, limit int) ([]metrics.QueryStat, error)

// MetricsHandler serves analytics endpoints.
type MetricsHandler struct {
	store    MetricsStore
	insights QueryInsightsFunc
}

// NewMetricsHandler builds a MetricsHandler. insights may be nil.
func NewMetricsHandler(store MetricsStore, insights QueryInsightsFunc) *MetricsHandler {
	return &MetricsHandler{store: store, insights: insights}
}

// Latest returns the most recent value per metric.
func (h *MetricsHandler) Latest(w http.ResponseWriter, r *http.Request) {
	latest, err := h.store.Latest(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": latest})
}

// Range returns samples for one metric over a time window. since/until are
// RFC3339; defaults are the last hour.
func (h *MetricsHandler) Range(w http.ResponseWriter, r *http.Request) {
	until, err := parseTimeParam(r, "until", time.Now())
	if err != nil {
		respondError(w, err)
		return
	}
	since, err := parseTimeParam(r, "since", until.Add(-time.Hour))
	if err != nil {
		respondError(w, err)
		return
	}

	samples, err := h.store.Query(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "metric"), since, until)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"samples": samples})
}

// Queries returns top queries (pg_stat_statements) for an instance.
func (h *MetricsHandler) Queries(w http.ResponseWriter, r *http.Request) {
	if h.insights == nil {
		writeJSON(w, http.StatusOK, map[string]any{"queries": []metrics.QueryStat{}})
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	queries, err := h.insights(r.Context(), chi.URLParam(r, "id"), limit)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queries": queries})
}

func parseTimeParam(r *http.Request, name string, def time.Time) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, apperr.New(apperr.KindInvalid, "invalid "+name+" timestamp (want RFC3339)")
	}
	return t, nil
}
