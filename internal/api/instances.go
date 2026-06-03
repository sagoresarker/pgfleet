package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// provisionTimeout bounds an async provision so a hung Docker/Postgres step
// can't leave a goroutine running forever (and writing to a closed pool/runtime
// after shutdown drains). Generous: pulling an image + initdb + stanza-create.
const provisionTimeout = 20 * time.Minute

// cloneTimeout bounds the full clone flow (source backup + restore + bring-up)
// so a hung pgBackRest operation can't leak the background goroutine.
const cloneTimeout = 30 * time.Minute

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
	MarkError(ctx context.Context, id, reason string) error
}

// ProgressSink receives provisioning progress for an instance (e.g. a WS hub).
type ProgressSink interface {
	Publish(instanceID, step, detail string)
}

// CloneBackupRunner takes a backup of an instance. It is satisfied by
// *backup.Service (its Run method) and lets the Clone handler capture a fresh
// full backup of the source before cloning, so the clone reflects the source's
// state at clone time.
type CloneBackupRunner interface {
	Run(ctx context.Context, instanceID, backupType string) error
}

// InstancesHandler serves managed-instance endpoints.
type InstancesHandler struct {
	store      InstanceStore
	prov       InstanceProvisioner
	progress   ProgressSink
	audit      AuditRecorder
	async      *Async
	cloneBakup CloneBackupRunner
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

// WithCloneBackup attaches the backup runner used to capture a fresh full
// backup of the source before a clone. If nil, Clone proceeds against whatever
// backup the source already has (and fails if it has none).
func (h *InstancesHandler) WithCloneBackup(r CloneBackupRunner) *InstancesHandler {
	h.cloneBakup = r
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
		ctx, cancel := context.WithTimeout(ctx, provisionTimeout)
		defer cancel()
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
	// REG-5: verify the instance exists before accepting the flip, so a bad/
	// nonexistent id 404s instead of returning a 202 for a no-op background task.
	if _, err := h.store.Get(r.Context(), id); err != nil {
		respondError(w, err)
		return
	}
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
		// A clone is a point-in-time copy: capture a fresh full backup of the
		// source first, so the clone reflects the source's state at clone time.
		// Bound the whole operation so a hung backup/restore can't leak the
		// goroutine. If the backup can't be produced, abort BEFORE Clone creates
		// any volumes/containers, and mark the target errored so it doesn't
		// linger in "provisioning".
		ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
		defer cancel()
		emit := func(step, detail string) {
			if progress != nil {
				progress(step, detail)
			}
		}
		if h.cloneBakup != nil {
			emit("backup", "taking a fresh backup of "+source.Name)
			if err := h.cloneBakup.Run(ctx, source.ID, "full"); err != nil {
				emit("error", "source backup failed: "+err.Error())
				_ = h.prov.MarkError(ctx, cloneID, "clone aborted: source backup failed: "+err.Error())
				return
			}
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
//
// Directly destroying any CLUSTER MEMBER is refused: tearing a primary out from
// under its replicas leaves the cluster headless, and tearing out a replica
// leaks its replication slot on the primary (which pins WAL). The operator must
// destroy the CLUSTER instead, which removes its members in the correct order
// and drops the slots. Standalones are unaffected.
func (h *InstancesHandler) Destroy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inst, err := h.store.Get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if inst.ClusterID != "" {
		respondError(w, apperr.New(apperr.KindConflict,
			"cannot destroy a cluster member directly; destroy the cluster "+inst.ClusterID+" instead (it tears down members in order and drops replication slots)"))
		return
	}
	retain := r.URL.Query().Get("retain_backups") == "true"
	// Detach the teardown from the request context: a client disconnect (closed
	// tab, proxy timeout) MUST NOT cancel Destroy mid-way — that would remove the
	// container but leave the data/repo volumes orphaned and the row stuck in
	// "error". WithoutCancel keeps request-scoped values but ignores cancellation.
	if err := h.prov.Destroy(context.WithoutCancel(r.Context()), id, retain); err != nil {
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
