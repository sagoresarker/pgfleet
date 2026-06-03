// Package api wires the control-plane HTTP surface.
package api

import (
	"context"
	"encoding/json"
	"errors"
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
	// SSO exchanges a trusted-header (Authelia/OIDC proxy) identity for a token
	// (optional; mounted only when configured).
	SSO *SSOHandler
	// Users serves admin user management.
	Users *UsersHandler
	// Audit serves read-only access to the append-only audit trail (admin-only).
	Audit *AuditHandler
	// Instances serves managed-instance endpoints.
	Instances *InstancesHandler
	// Clusters serves HA cluster endpoints.
	Clusters *ClustersHandler
	// Backups serves backup/restore endpoints.
	Backups *BackupsHandler
	// Metrics serves analytics endpoints.
	Metrics *MetricsHandler
	// Health serves the fleet health and alerts view.
	Health *HealthHandler
	// Events is the WebSocket hub for live progress (optional).
	Events *ws.Hub
	// Timescale serves TimescaleDB management endpoints (optional).
	Timescale *TimescaleHandler
	// Alerts serves the active-alerts view (optional).
	Alerts *AlertsHandler
	// AlertRules serves user-configurable alert-rule CRUD (optional).
	AlertRules *AlertRulesHandler
	// EventsHistory serves the persisted event timeline (optional).
	EventsHistory *EventsHistoryHandler
	// Logs serves instance container logs (optional).
	Logs *LogsHandler
	// Prometheus serves the /metrics exposition endpoint (optional). It is
	// mounted unauthenticated at /metrics by Prometheus convention; restrict it
	// at the network layer.
	Prometheus *PrometheusHandler
	// SQL runs ad-hoc queries from the dashboard (optional).
	SQL *SQLHandler
	// Exec runs one-shot container commands (optional).
	Exec *ExecHandler
	// Dump streams a logical pg_dump download (optional).
	Dump *DumpHandler
	// Remote serves the migrate-in (remote backup & restore) endpoints (optional).
	Remote *RemoteHandler
	// Pool serves live PgCat router pool stats (optional).
	Pool *PoolHandler
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
	// Prometheus scrape endpoint: unauthenticated by convention (scrapers do not
	// send bearer tokens) — restrict it at the network layer.
	if deps.Prometheus != nil {
		r.Get("/metrics", deps.Prometheus.Metrics)
	}

	r.Route("/api/v1", func(api chi.Router) {
		if deps.Auth != nil {
			api.Post("/auth/login", deps.Auth.Login)
		}
		// SSO exchange is the entry point for forward-auth: it is authenticated by
		// the trusted upstream header (set by the IdP proxy), not a bearer token,
		// so it sits outside the bearer-auth group.
		if deps.SSO != nil {
			api.Post("/auth/sso", deps.SSO.Exchange)
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
				if deps.Clusters != nil {
					mountClusterRoutes(pr, deps.Clusters)
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
				if deps.Alerts != nil {
					pr.Group(func(ar chi.Router) {
						ar.Use(auth.RequireAction(auth.ActionMetricsRead))
						ar.Get("/alerts", deps.Alerts.List)
					})
				}
				if deps.AlertRules != nil {
					// Listing rules is read-level; mutating them is write-level.
					pr.Group(func(rr chi.Router) {
						rr.Use(auth.RequireAction(auth.ActionMetricsRead))
						rr.Get("/alert-rules", deps.AlertRules.List)
					})
					pr.Group(func(rr chi.Router) {
						rr.Use(auth.RequireAction(auth.ActionInstanceWrite))
						rr.Post("/alert-rules", deps.AlertRules.Create)
						rr.Put("/alert-rules/{id}", deps.AlertRules.Update)
						rr.Delete("/alert-rules/{id}", deps.AlertRules.Delete)
					})
				}
				if deps.EventsHistory != nil {
					pr.Group(func(er chi.Router) {
						er.Use(auth.RequireAction(auth.ActionMetricsRead))
						er.Get("/events/history", deps.EventsHistory.List)
					})
				}
				if deps.Logs != nil {
					pr.Group(func(lr chi.Router) {
						lr.Use(auth.RequireAction(auth.ActionInstanceRead))
						lr.Get("/instances/{id}/logs", deps.Logs.Get)
					})
				}
				if deps.Dump != nil {
					// A logical dump exposes all data, like the DSN — gate at the
					// connection level.
					pr.Group(func(dr chi.Router) {
						dr.Use(auth.RequireAction(auth.ActionInstanceConnect))
						dr.Get("/instances/{id}/dump", deps.Dump.Get)
					})
				}
				if deps.Timescale != nil {
					mountTimescaleRoutes(pr, deps.Timescale)
				}
				if deps.SQL != nil {
					// Ad-hoc SQL runs as the superuser, so it is gated at the
					// connection level (operator/admin), like revealing the DSN.
					pr.Group(func(sr chi.Router) {
						sr.Use(auth.RequireAction(auth.ActionInstanceConnect))
						sr.Post("/instances/{id}/sql", deps.SQL.Run)
					})
				}
				if deps.Exec != nil {
					// SEC-1: gating across the three privileged data-plane
					// endpoints is deliberately set so that NONE are reachable by
					// read-only viewers. SQL and Dump are gated at the connect
					// level (they are DSN-equivalent: full data access as the
					// superuser). Exec is gated STRICTER, at the write level,
					// because it runs ARBITRARY commands as root inside the
					// container — strictly more dangerous than a DB query or a
					// logical dump (it can touch the filesystem, processes, and
					// anything else in the container, not just the database).
					// Keeping exec stricter is the safe, intentional choice, not
					// an accident.
					pr.Group(func(xr chi.Router) {
						xr.Use(auth.RequireAction(auth.ActionInstanceWrite))
						xr.Post("/instances/{id}/exec", deps.Exec.Run)
					})
				}
				if deps.Remote != nil {
					mountRemoteRoutes(pr, deps.Remote)
				}
				if deps.Pool != nil {
					pr.Group(func(mr chi.Router) {
						mr.Use(auth.RequireAction(auth.ActionMetricsRead))
						mr.Get("/clusters/{id}/pool/stats", deps.Pool.Stats)
					})
				}
				if deps.Audit != nil {
					// The audit log records who did what across the whole fleet,
					// including privileged data-plane actions, so reading it is
					// admin-only (same bar as managing users).
					pr.Group(func(ar chi.Router) {
						ar.Use(auth.RequireAction(auth.ActionUserManage))
						ar.Get("/audit", deps.Audit.List)
					})
				}
			})
		}

		// WebSocket events authenticate via a query-param token (browsers
		// cannot set headers on WS), so they sit outside the header-auth group.
		// Beyond verifying the token signature, authorize the ROLE: the live
		// progress stream is the same fleet data as the persisted events timeline,
		// so it requires the same read permission (defense-in-depth — a validly
		// signed token with an unprivileged role must not subscribe).
		if deps.Events != nil && deps.Issuer != nil {
			api.Get("/events", ws.Handler(deps.Events, func(token string) error {
				claims, err := deps.Issuer.Verify(token)
				if err != nil {
					return err
				}
				if !auth.Can(claims.Role, auth.ActionMetricsRead) {
					return errInsufficientPermissions
				}
				return nil
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
	})
	// The connection DSN contains the plaintext superuser password, so it is
	// gated above viewer level (operator/admin only).
	pr.Group(func(cr chi.Router) {
		cr.Use(auth.RequireAction(auth.ActionInstanceConnect))
		cr.Get("/instances/{id}/connection", h.Connection)
	})
	pr.Group(func(wr chi.Router) {
		wr.Use(auth.RequireAction(auth.ActionInstanceWrite))
		wr.Post("/instances", h.Create)
		wr.Post("/instances/{id}/clone", h.Clone)
		wr.Post("/instances/{id}/visibility", h.Visibility)
		wr.Post("/instances/{id}/start", h.Start)
		wr.Post("/instances/{id}/stop", h.Stop)
		wr.Post("/instances/{id}/restart", h.Restart)
	})
	pr.Group(func(dr chi.Router) {
		dr.Use(auth.RequireAction(auth.ActionInstanceDelete))
		dr.Delete("/instances/{id}", h.Destroy)
	})
}

func mountClusterRoutes(pr chi.Router, h *ClustersHandler) {
	pr.Group(func(rr chi.Router) {
		rr.Use(auth.RequireAction(auth.ActionInstanceRead))
		rr.Get("/clusters", h.List)
		rr.Get("/clusters/{id}", h.Get)
	})
	pr.Group(func(cr chi.Router) {
		cr.Use(auth.RequireAction(auth.ActionInstanceConnect))
		cr.Get("/clusters/{id}/connection", h.Connection)
	})
	pr.Group(func(wr chi.Router) {
		wr.Use(auth.RequireAction(auth.ActionInstanceWrite))
		wr.Post("/clusters", h.Create)
	})
	pr.Group(func(dr chi.Router) {
		dr.Use(auth.RequireAction(auth.ActionInstanceDelete))
		dr.Delete("/clusters/{id}", h.Destroy)
	})
}

func mountTimescaleRoutes(pr chi.Router, h *TimescaleHandler) {
	base := "/instances/{id}/timescale"
	// Reads: list hypertables + background jobs.
	pr.Group(func(rr chi.Router) {
		rr.Use(auth.RequireAction(auth.ActionInstanceRead))
		rr.Get(base+"/hypertables", h.List)
		rr.Get(base+"/jobs", h.Jobs)
	})
	// Writes: create hypertables + manage retention/compression/aggregates.
	pr.Group(func(wr chi.Router) {
		wr.Use(auth.RequireAction(auth.ActionInstanceWrite))
		wr.Post(base+"/hypertables", h.CreateHypertable)
		wr.Post(base+"/retention", h.AddRetention)
		wr.Delete(base+"/retention", h.RemoveRetention)
		wr.Post(base+"/compression", h.EnableCompression)
		wr.Delete(base+"/compression", h.RemoveCompression)
		wr.Post(base+"/continuous-aggregates", h.CreateContinuousAggregate)
	})
}

// errInsufficientPermissions rejects a WebSocket handshake whose token is valid
// but whose role lacks the required permission.
var errInsufficientPermissions = errors.New("insufficient permissions")

// mountRemoteRoutes wires the migrate-in (remote backup & restore) endpoints.
// Capturing a remote dump and restoring it into a freshly provisioned target
// both create/own managed resources, so they require the same write privilege as
// creating an instance/cluster; the list is a read.
func mountRemoteRoutes(pr chi.Router, h *RemoteHandler) {
	pr.Group(func(rr chi.Router) {
		rr.Use(auth.RequireAction(auth.ActionInstanceRead))
		rr.Get("/remote/backups", h.List)
	})
	pr.Group(func(wr chi.Router) {
		wr.Use(auth.RequireAction(auth.ActionInstanceWrite))
		wr.Post("/remote/backups", h.Capture)
		wr.Post("/remote/backups/{id}/restore", h.Restore)
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
		// verify is read-only on the repo (an integrity check), so it sits at the
		// backup-write level, not the stricter restore/delete level.
		wr.Post("/instances/{id}/backups/verify", h.Verify)
	})
	pr.Group(func(rs chi.Router) {
		// Restore and single-backup deletion both permanently change recovery
		// state, so they share the most-privileged backup action.
		rs.Use(auth.RequireAction(auth.ActionBackupRestore))
		rs.Post("/instances/{id}/restore", h.Restore)
		rs.Delete("/instances/{id}/backups/{label}", h.Delete)
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
