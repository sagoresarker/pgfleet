package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/pgcat"
)

// PoolStatsReader reads live pool statistics from a router's admin interface.
// Implemented by a thin adapter over pgcat.ReadPoolStats; faked in tests.
type PoolStatsReader interface {
	ReadPoolStats(ctx context.Context, adminDSN string) (pgcat.PoolStats, error)
}

// RouterAdminResolver returns the PgCat admin DSN for a cluster's router
// (admin user/password on the router host:port, database "pgcat"). It returns
// an apperr KindNotFound if the cluster or its router does not exist.
type RouterAdminResolver interface {
	RouterAdminDSN(ctx context.Context, clusterID, host string) (string, error)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"pools": stats.Pools,
		"stats": stats.Stats,
	})
}

// PoolStatsReaderFunc adapts pgcat.ReadPoolStats (or a fake) to PoolStatsReader.
type PoolStatsReaderFunc func(ctx context.Context, adminDSN string) (pgcat.PoolStats, error)

// ReadPoolStats implements PoolStatsReader.
func (f PoolStatsReaderFunc) ReadPoolStats(ctx context.Context, adminDSN string) (pgcat.PoolStats, error) {
	return f(ctx, adminDSN)
}
