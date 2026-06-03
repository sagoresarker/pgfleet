package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// InstanceStore is the subset of the instance repository the handler needs.
type InstanceStore interface {
	Create(ctx context.Context, in instance.NewInstance) (instance.Instance, error)
	Get(ctx context.Context, id string) (instance.Instance, error)
	List(ctx context.Context) ([]instance.Instance, error)
}

// InstanceProvisioner orchestrates instance containers.
type InstanceProvisioner interface {
	Provision(ctx context.Context, id string, progress provision.ProgressFunc) error
	Clone(ctx context.Context, cloneID string, source instance.Instance, progress provision.ProgressFunc) error
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	Destroy(ctx context.Context, id string, retainBackups bool) error
	SetVisibility(ctx context.Context, id string, public bool) error
	DSN(ctx context.Context, id string) (string, error)
}

// ProgressSink receives provisioning progress for an instance (e.g. a WS hub).
type ProgressSink interface {
	Publish(instanceID, step, detail string)
}

// InstancesHandler serves managed-instance endpoints.
type InstancesHandler struct {
	store    InstanceStore
	prov     InstanceProvisioner
	progress ProgressSink
	audit    AuditRecorder
	async    *Async
}

// NewInstancesHandler builds an InstancesHandler. progress may be nil.
func NewInstancesHandler(store InstanceStore, prov InstanceProvisioner, progress ProgressSink) *InstancesHandler {
	return &InstancesHandler{store: store, prov: prov, progress: progress}
}

// WithAudit attaches an audit recorder.
func (h *InstancesHandler) WithAudit(rec AuditRecorder) *InstancesHandler {
	h.audit = rec
	return h
}

// WithAsync attaches the background-task tracker for graceful shutdown.
func (h *InstancesHandler) WithAsync(a *Async) *InstancesHandler {
	h.async = a
	return h
}

type createInstanceRequest struct {
	Name       string            `json:"name"`
	RepoType   string            `json:"repo_type"`
	PGVersion  string            `json:"pg_version"`
	Password   string            `json:"password"`
	Parameters map[string]string `json:"parameters"`
	Extensions []string          `json:"extensions"`
}

type instancePayload struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	RepoType   string            `json:"repo_type"`
	PGVersion  string            `json:"pg_version"`
	HostPort   int               `json:"host_port"`
	Stanza     string            `json:"stanza"`
	Role       string            `json:"role"`
	ClusterID  string            `json:"cluster_id,omitempty"`
	LastError  string            `json:"last_error,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Extensions []string          `json:"extensions,omitempty"`
	Public     bool              `json:"public"`
}

func toInstancePayload(i instance.Instance) instancePayload {
	return instancePayload{
		ID: i.ID, Name: i.Name, Status: string(i.Status), RepoType: string(i.RepoType),
		PGVersion: i.PGVersion, HostPort: i.HostPort, Stanza: i.Stanza,
		Role: string(i.Role), ClusterID: i.ClusterID, LastError: i.LastError,
		Parameters: i.Parameters, Extensions: i.Extensions, Public: i.Public,
	}
}

// Create validates input, persists the instance, and provisions it
// asynchronously, returning 202 Accepted.
func (h *InstancesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createInstanceRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	inst, err := h.store.Create(r.Context(), instance.NewInstance{
		Name:       req.Name,
		PGVersion:  req.PGVersion,
		RepoType:   instance.RepoType(req.RepoType),
		Password:   req.Password,
		Parameters: req.Parameters,
		Extensions: req.Extensions,
	})
	if err != nil {
		respondError(w, err)
		return
	}

	recordAudit(h.audit, r, "instance.create", inst.Name)
	id := inst.ID
	h.async.Go(func(ctx context.Context) {
		progress := provision.ProgressFunc(nil)
		if h.progress != nil {
			progress = func(step, detail string) { h.progress.Publish(id, step, detail) }
		}
		_ = h.prov.Provision(ctx, id, progress)
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"instance": toInstancePayload(inst)})
}

type visibilityRequest struct {
	Public bool `json:"public"`
}

// Visibility toggles whether the instance's port is publicly reachable; the
// container is recreated with the new binding asynchronously (202).
func (h *InstancesHandler) Visibility(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req visibilityRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "instance.visibility", id)
	h.async.Go(func(ctx context.Context) { _ = h.prov.SetVisibility(ctx, id, req.Public) })
	w.WriteHeader(http.StatusAccepted)
}

type cloneRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// Clone provisions a new instance whose data is a full copy of the source
// (restored from the source's backup repo), returning 202.
func (h *InstancesHandler) Clone(w http.ResponseWriter, r *http.Request) {
	source, err := h.store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	var req cloneRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	// The clone inherits the source's repo type, version, params, and
	// extensions; it gets its own name + password.
	clone, err := h.store.Create(r.Context(), instance.NewInstance{
		Name:       req.Name,
		PGVersion:  source.PGVersion,
		RepoType:   source.RepoType,
		Password:   req.Password,
		Parameters: source.Parameters,
		Extensions: source.Extensions,
	})
	if err != nil {
		respondError(w, err)
		return
	}

	recordAudit(h.audit, r, "instance.clone", clone.Name+" from "+source.Name)
	cloneID := clone.ID
	h.async.Go(func(ctx context.Context) {
		progress := provision.ProgressFunc(nil)
		if h.progress != nil {
			progress = func(step, detail string) { h.progress.Publish(cloneID, step, detail) }
		}
		_ = h.prov.Clone(ctx, cloneID, source, progress)
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"instance": toInstancePayload(clone)})
}

// List returns all instances.
func (h *InstancesHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	payloads := make([]instancePayload, 0, len(items))
	for _, i := range items {
		payloads = append(payloads, toInstancePayload(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": payloads})
}

// Get returns one instance.
func (h *InstancesHandler) Get(w http.ResponseWriter, r *http.Request) {
	inst, err := h.store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": toInstancePayload(inst)})
}

// Start starts a stopped instance.
func (h *InstancesHandler) Start(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.prov.Start, "instance.start")
}

// Stop stops a running instance.
func (h *InstancesHandler) Stop(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.prov.Stop, "instance.stop")
}

// Restart restarts an instance.
func (h *InstancesHandler) Restart(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.prov.Restart, "instance.restart")
}

func (h *InstancesHandler) lifecycle(w http.ResponseWriter, r *http.Request, fn func(context.Context, string) error, action string) {
	id := chi.URLParam(r, "id")
	if err := fn(r.Context(), id); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, action, id)
	w.WriteHeader(http.StatusNoContent)
}

// Destroy removes an instance. ?retain_backups=true keeps the backup repo.
func (h *InstancesHandler) Destroy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	retain := r.URL.Query().Get("retain_backups") == "true"
	if err := h.prov.Destroy(r.Context(), id, retain); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, "instance.destroy", id)
	w.WriteHeader(http.StatusNoContent)
}

// Connection returns the instance's client DSN.
func (h *InstancesHandler) Connection(w http.ResponseWriter, r *http.Request) {
	dsn, err := h.prov.DSN(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"dsn": dsn})
}
