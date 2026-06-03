package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// ClusterService orchestrates clusters.
type ClusterService interface {
	Create(ctx context.Context, in clusterctl.Input) (cluster.Cluster, error)
	Provision(ctx context.Context, id string, progress provision.ProgressFunc) error
	Destroy(ctx context.Context, id string, retainBackups bool) error
	ConnectionDSN(ctx context.Context, clusterID, host string) (string, error)
}

// ClusterStore reads clusters.
type ClusterStore interface {
	List(ctx context.Context) ([]cluster.Cluster, error)
	Get(ctx context.Context, id string) (cluster.Cluster, error)
}

// MemberLister lists a cluster's member instances.
type MemberLister interface {
	ListByCluster(ctx context.Context, clusterID string) ([]instance.Instance, error)
}

// ClustersHandler serves HA cluster endpoints.
type ClustersHandler struct {
	svc     ClusterService
	store   ClusterStore
	members MemberLister
	host    string
	audit   AuditRecorder
	async   *Async
}

// NewClustersHandler builds a ClustersHandler. audit may be nil.
func NewClustersHandler(svc ClusterService, store ClusterStore, members MemberLister, host string, rec AuditRecorder) *ClustersHandler {
	return &ClustersHandler{svc: svc, store: store, members: members, host: host, audit: rec}
}

// WithAsync attaches the background-task tracker.
func (h *ClustersHandler) WithAsync(a *Async) *ClustersHandler {
	h.async = a
	return h
}

type createClusterRequest struct {
	Name       string            `json:"name"`
	Replicas   int               `json:"replicas"`
	Password   string            `json:"password"`
	RepoType   string            `json:"repo_type"`
	PGVersion  string            `json:"pg_version"`
	Parameters map[string]string `json:"parameters"`
	Extensions []string          `json:"extensions"`
}

type clusterPayload struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	RouterPort int    `json:"router_port"`
	LastError  string `json:"last_error,omitempty"`
}

func toClusterPayload(c cluster.Cluster) clusterPayload {
	return clusterPayload{ID: c.ID, Name: c.Name, Status: string(c.Status), RouterPort: c.RouterPort, LastError: c.LastError}
}

// Create persists a cluster and provisions it asynchronously (202).
func (h *ClustersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createClusterRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if len(req.Password) < 8 {
		respondError(w, apperr.New(apperr.KindInvalid, "password must be at least 8 characters"))
		return
	}
	c, err := h.svc.Create(r.Context(), clusterctl.Input{
		Name: req.Name, Replicas: req.Replicas, Password: req.Password,
		RepoType: instance.RepoType(req.RepoType), Version: req.PGVersion,
		Parameters: req.Parameters, Extensions: req.Extensions,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "cluster.create", c.Name)
	id := c.ID
	h.async.Go(func(ctx context.Context) { _ = h.svc.Provision(ctx, id, nil) })
	writeJSON(w, http.StatusAccepted, map[string]any{"cluster": toClusterPayload(c)})
}

// List returns all clusters.
func (h *ClustersHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	payloads := make([]clusterPayload, 0, len(items))
	for _, c := range items {
		payloads = append(payloads, toClusterPayload(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clusters": payloads})
}

// Get returns a cluster plus its members.
func (h *ClustersHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := h.store.Get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	members, err := h.members.ListByCluster(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	memberPayloads := make([]instancePayload, 0, len(members))
	for _, m := range members {
		p := toInstancePayload(m)
		memberPayloads = append(memberPayloads, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cluster": toClusterPayload(c),
		"members": memberPayloads,
	})
}

// Destroy tears down a cluster. ?retain_backups=true keeps repos.
func (h *ClustersHandler) Destroy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	retain := r.URL.Query().Get("retain_backups") == "true"
	if err := h.svc.Destroy(r.Context(), id, retain); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "cluster.destroy", id)
	w.WriteHeader(http.StatusNoContent)
}

// Connection returns the cluster's router DSN.
func (h *ClustersHandler) Connection(w http.ResponseWriter, r *http.Request) {
	dsn, err := h.svc.ConnectionDSN(r.Context(), chi.URLParam(r, "id"), h.host)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dsn": dsn})
}
