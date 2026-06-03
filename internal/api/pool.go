package api

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/pgcat"
)

// PoolStatsReader reads live pool statistics from a router's admin interface.
// Implemented by a thin adapter over pgcat.ReadPoolStats; faked in tests.
type PoolStatsReader interface {
	ReadPoolStats(ctx context.Context, adminDSN string) (pgcat.PoolStats, error)
}

// RouterAdminResolver returns the PgCat admin DSN for a cluster's router
// (admin user/password on the router host:port, database "pgcat") plus the
// cluster's backend topology so live SHOW SERVERS traffic can be attributed to
// the primary vs replicas. RouterAdminDSN returns an apperr KindNotFound if the
// cluster or its router does not exist.
type RouterAdminResolver interface {
	RouterAdminDSN(ctx context.Context, clusterID, host string) (string, error)
	RouterBackends(ctx context.Context, clusterID string) ([]clusterctl.RouterBackend, error)
}

// RoutingBackend is one member of the router pool with live traffic attributed
// to it from SHOW SERVERS. Connections counts the server connections the router
// currently holds to this backend (Active = state "active"); the byte/query
// counters are summed across those connections as an instantaneous load share.
type RoutingBackend struct {
	Name             string `json:"name"`
	Role             string `json:"role"`
	Address          string `json:"address"`
	Connections      int    `json:"connections"`
	ActiveConns      int    `json:"active_connections"`
	QueryCount       int64  `json:"query_count"`
	TransactionCount int64  `json:"transaction_count"`
	BytesSent        int64  `json:"bytes_sent"`
	BytesReceived    int64  `json:"bytes_received"`
}

// buildRouting attributes each SHOW SERVERS row to a cluster backend and sums
// its counters, returning one RoutingBackend per member in the given order
// (primary first). A server is matched to the backend whose configured address
// is the longest substring of the server's address_name, so a member whose name
// is a prefix of another's (orders vs orders-2) can't steal the other's rows.
func buildRouting(backends []clusterctl.RouterBackend, servers []pgcat.ServerStat) []RoutingBackend {
	out := make([]RoutingBackend, len(backends))
	byName := make(map[string]*RoutingBackend, len(backends))
	for i, b := range backends {
		out[i] = RoutingBackend{Name: b.Name, Role: b.Role, Address: b.Address}
		byName[b.Name] = &out[i]
	}

	// Match longest address first so a longer, more-specific member address wins
	// over one that is its prefix.
	matchOrder := slices.Clone(backends)
	sort.SliceStable(matchOrder, func(i, j int) bool {
		return len(matchOrder[i].Address) > len(matchOrder[j].Address)
	})

	for _, srv := range servers {
		for _, b := range matchOrder {
			if b.Address == "" || !strings.Contains(srv.Address, b.Address) {
				continue
			}
			agg := byName[b.Name]
			agg.Connections++
			if srv.State == "active" {
				agg.ActiveConns++
			}
			agg.QueryCount += srv.QueryCount
			agg.TransactionCount += srv.TransactionCount
			agg.BytesSent += srv.BytesSent
			agg.BytesReceived += srv.BytesReceived
			break
		}
	}
	return out
}

// PoolHandler serves live router pool stats.
type PoolHandler struct {
	reader   PoolStatsReader
	resolver RouterAdminResolver
	host     string
}

// NewPoolHandler builds a PoolHandler. host is the externally reachable host of
// the router (used to build the admin DSN).
func NewPoolHandler(reader PoolStatsReader, resolver RouterAdminResolver, host string) *PoolHandler {
	return &PoolHandler{reader: reader, resolver: resolver, host: host}
}

// Stats handles GET /clusters/{id}/pool/stats: it resolves the cluster's router
// admin DSN, reads SHOW POOLS / SHOW STATS, and returns them as JSON. Gate this
// route at auth.ActionMetricsRead.
func (h *PoolHandler) Stats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	dsn, err := h.resolver.RouterAdminDSN(r.Context(), id, h.host)
	if err != nil {
		respondError(w, err)
		return
	}
	stats, err := h.reader.ReadPoolStats(r.Context(), dsn)
	if err != nil {
		respondError(w, err)
		return
	}
	// Routing is best-effort enrichment: if the topology lookup fails we still
	// return the raw pool/stats/clients so the core panel keeps working.
	routing := []RoutingBackend{}
	if backends, err := h.resolver.RouterBackends(r.Context(), id); err == nil {
		routing = buildRouting(backends, stats.Servers)
	}
	// Normalize nil slices to [] so the JSON is arrays, never null (the frontend
	// iterates these directly).
	writeJSON(w, http.StatusOK, map[string]any{
		"pools":   orEmpty(stats.Pools),
		"stats":   orEmpty(stats.Stats),
		"servers": orEmpty(stats.Servers),
		"clients": orEmpty(stats.Clients),
		"routing": routing,
	})
}

// orEmpty returns a non-nil slice so JSON encodes [] instead of null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// PoolStatsReaderFunc adapts pgcat.ReadPoolStats (or a fake) to PoolStatsReader.
type PoolStatsReaderFunc func(ctx context.Context, adminDSN string) (pgcat.PoolStats, error)

// ReadPoolStats implements PoolStatsReader.
func (f PoolStatsReaderFunc) ReadPoolStats(ctx context.Context, adminDSN string) (pgcat.PoolStats, error) {
	return f(ctx, adminDSN)
}
