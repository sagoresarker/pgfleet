// Package api wires the control-plane HTTP surface.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sagoresarker/pgfleet/internal/auth"
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
}

// NewRouter builds the control-plane HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

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
			})
		}
	})

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
