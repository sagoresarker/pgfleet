// Package api wires the control-plane HTTP surface.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ReadyFunc reports whether the control plane is ready to serve traffic
// (e.g. the meta database is reachable). A nil ReadyFunc is treated as ready.
type ReadyFunc func(ctx context.Context) error

// Deps holds the collaborators the router needs.
type Deps struct {
	// Ready is the readiness probe used by /readyz.
	Ready ReadyFunc
}

// NewRouter builds the control-plane HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(deps.Ready))

	return r
}

// handleHealthz is a liveness probe: it always reports OK and must not depend
// on downstream dependencies.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz reports readiness based on the injected check.
func handleReadyz(ready ReadyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": "unavailable",
					"error":  err.Error(),
				})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
