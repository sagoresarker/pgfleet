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
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	Destroy(ctx context.Context, id string, retainBackups bool) error
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

type createInstanceRequest struct {
	Name      string `json:"name"`
	RepoType  string `json:"repo_type"`
	PGVersion string `json:"pg_version"`
	Password  string `json:"password"`
}

type instancePayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	RepoType  string `json:"repo_type"`
	PGVersion string `json:"pg_version"`
	HostPort  int    `json:"host_port"`
	Stanza    string `json:"stanza"`
	Role      string `json:"role"`
	ClusterID string `json:"cluster_id,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

func toInstancePayload(i instance.Instance) instancePayload {
	return instancePayload{
		ID: i.ID, Name: i.Name, Status: string(i.Status), RepoType: string(i.RepoType),
		PGVersion: i.PGVersion, HostPort: i.HostPort, Stanza: i.Stanza,
		Role: string(i.Role), ClusterID: i.ClusterID, LastError: i.LastError,
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
		Name:      req.Name,
		PGVersion: req.PGVersion,
		RepoType:  instance.RepoType(req.RepoType),
		Password:  req.Password,
	})
	if err != nil {
		respondError(w, err)
		return
	}

	recordAudit(h.audit, r, "instance.create", inst.Name)
	go h.provisionAsync(inst.ID)

	writeJSON(w, http.StatusAccepted, map[string]any{"instance": toInstancePayload(inst)})
}

// provisionAsync runs provisioning in the background, publishing progress.
func (h *InstancesHandler) provisionAsync(id string) {
	progress := provision.ProgressFunc(nil)
	if h.progress != nil {
		progress = func(step, detail string) { h.progress.Publish(id, step, detail) }
	}
	_ = h.prov.Provision(context.Background(), id, progress)
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
