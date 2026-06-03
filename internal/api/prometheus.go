package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/metrics"
)

type promInstanceLister interface {
	List(ctx context.Context) ([]instance.Instance, error)
}

type promMetricStore interface {
	Latest(ctx context.Context, instanceID string) (map[string]metrics.Sample, error)
}

// PrometheusHandler exposes the latest per-instance metrics in the Prometheus
// text exposition format so operators can scrape PgFleet into existing
// Grafana/Alertmanager stacks.
type PrometheusHandler struct {
	instances promInstanceLister
	store     promMetricStore
}

// NewPrometheusHandler builds a PrometheusHandler.
func NewPrometheusHandler(instances promInstanceLister, store promMetricStore) *PrometheusHandler {
	return &PrometheusHandler{instances: instances, store: store}
}

// Metrics writes the latest sample of every metric for every instance.
func (h *PrometheusHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	insts, err := h.instances.List(r.Context())
	if err != nil {
		http.Error(w, "# failed to list instances\n", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder
	for _, inst := range insts {
		latest, err := h.store.Latest(r.Context(), inst.ID)
		if err != nil {
			continue // best-effort per instance
		}
		// Deterministic ordering for stable scrapes/diffs.
		names := make([]string, 0, len(latest))
		for name := range latest {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "pgfleet_%s{instance_id=%q,name=%q} %g\n",
				promName(name), inst.ID, inst.Name, latest[name].Value)
		}
	}
	_, _ = w.Write([]byte(b.String()))
}

// promName sanitises a metric name to the Prometheus charset ([a-zA-Z0-9_:]).
func promName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
