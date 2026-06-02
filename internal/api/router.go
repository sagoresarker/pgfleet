// Package api wires the control-plane HTTP surface.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"

	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/ws"
)

// ReadyFunc reports whether the control plane is ready to serve traffic
// (e.g. the meta database is reachable). A nil ReadyFunc is treated as ready.
type ReadyFunc func(ctx context.Context) error

// Deps holds the collaborators the router needs. Auth-related fields are
// optional; when nil their routes are not mounted (useful for health-only
// tests).
type Deps struct {
	// Ready is the readiness probe used by /readyz.
	Ready ReadyFunc
	// Issuer verifies bearer tokens for protected routes.
	Issuer *auth.Issuer
	// Auth serves login/logout.
	Auth *AuthHandler
	// Users serves admin user management.
	Users *UsersHandler
	// Instances serves managed-instance endpoints.
	Instances *InstancesHandler
	// Backups serves backup/restore endpoints.
	Backups *BackupsHandler
	// Metrics serves analytics endpoints.
	Metrics *MetricsHandler
	// Health serves the fleet health and alerts view.
	Health *HealthHandler
	// Events is the WebSocket hub for live progress (optional).
	Events *ws.Hub
}

// NewRouter builds the control-plane HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(httprate.LimitByIP(120, time.Minute)) // 120 req/min per client

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(deps.Ready))

	r.Route("/api/v1", func(api chi.Router) {
		if deps.Auth != nil {
			api.Post("/auth/login", deps.Auth.Login)
		}
		if deps.Issuer != nil {
			api.Group(func(pr chi.Router) {
				pr.Use(deps.Issuer.Authenticate)

				if deps.Auth != nil {
					pr.Post("/auth/logout", deps.Auth.Logout)
				}
				if deps.Users != nil {
					pr.Group(func(ur chi.Router) {
						ur.Use(auth.RequireAction(auth.ActionUserManage))
						ur.Post("/users", deps.Users.Create)
						ur.Get("/users", deps.Users.List)
						ur.Post("/users/{id}/disable", deps.Users.Disable)
						ur.Post("/users/{id}/enable", deps.Users.Enable)
					})
				}
				if deps.Instances != nil {
					mountInstanceRoutes(pr, deps.Instances)
				}
				if deps.Backups != nil {
					mountBackupRoutes(pr, deps.Backups)
				}
				if deps.Metrics != nil {
					pr.Group(func(mr chi.Router) {
						mr.Use(auth.RequireAction(auth.ActionMetricsRead))
						mr.Get("/instances/{id}/metrics", deps.Metrics.Latest)
						mr.Get("/instances/{id}/metrics/{metric}", deps.Metrics.Range)
						mr.Get("/instances/{id}/queries", deps.Metrics.Queries)
					})
				}
				if deps.Health != nil {
					pr.Group(func(hr chi.Router) {
						hr.Use(auth.RequireAction(auth.ActionMetricsRead))
						hr.Get("/health", deps.Health.List)
					})
				}
			})
		}

		// WebSocket events authenticate via a query-param token (browsers
		// cannot set headers on WS), so they sit outside the header-auth group.
		if deps.Events != nil && deps.Issuer != nil {
			api.Get("/events", ws.Handler(deps.Events, func(token string) error {
				_, err := deps.Issuer.Verify(token)
				return err
			}))
		}
	})

	return r
}

func mountInstanceRoutes(pr chi.Router, h *InstancesHandler) {
	pr.Group(func(rr chi.Router) {
		rr.Use(auth.RequireAction(auth.ActionInstanceRead))
		rr.Get("/instances", h.List)
		rr.Get("/instances/{id}", h.Get)
		rr.Get("/instances/{id}/connection", h.Connection)
	})
	pr.Group(func(wr chi.Router) {
		wr.Use(auth.RequireAction(auth.ActionInstanceWrite))
		wr.Post("/instances", h.Create)
		wr.Post("/instances/{id}/start", h.Start)
		wr.Post("/instances/{id}/stop", h.Stop)
		wr.Post("/instances/{id}/restart", h.Restart)
	})
	pr.Group(func(dr chi.Router) {
		dr.Use(auth.RequireAction(auth.ActionInstanceDelete))
		dr.Delete("/instances/{id}", h.Destroy)
	})
}

func mountBackupRoutes(pr chi.Router, h *BackupsHandler) {
	pr.Group(func(rr chi.Router) {
		rr.Use(auth.RequireAction(auth.ActionBackupRead))
		rr.Get("/instances/{id}/backups", h.List)
	})
	pr.Group(func(wr chi.Router) {
		wr.Use(auth.RequireAction(auth.ActionBackupWrite))
		wr.Post("/instances/{id}/backups", h.Create)
	})
	pr.Group(func(rs chi.Router) {
		rs.Use(auth.RequireAction(auth.ActionBackupRestore))
		rs.Post("/instances/{id}/restore", h.Restore)
	})
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
