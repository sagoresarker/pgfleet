package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// BackupRunner takes backups, deletes a single backup, verifies the repo, and
// lists the catalog.
type BackupRunner interface {
	RunWith(ctx context.Context, instanceID, backupType string, opts backup.RunOpts) error
	Delete(ctx context.Context, instanceID, label string) error
	Verify(ctx context.Context, instanceID string) error
	List(ctx context.Context, instanceID string) ([]backup.Backup, error)
}

// Restorer restores an instance (including PITR).
type Restorer interface {
	Restore(ctx context.Context, instanceID string, opts provision.RestoreOptions, progress provision.ProgressFunc) error
}

// BackupsHandler serves backup/restore endpoints.
type BackupsHandler struct {
	runner   BackupRunner
	restorer Restorer
	audit    AuditRecorder
	async    *Async
}

// NewBackupsHandler builds a BackupsHandler. audit may be nil.
func NewBackupsHandler(runner BackupRunner, restorer Restorer, rec AuditRecorder) *BackupsHandler {
	return &BackupsHandler{runner: runner, restorer: restorer, audit: rec}
}

// WithAsync attaches the background-task tracker.
func (h *BackupsHandler) WithAsync(a *Async) *BackupsHandler {
	h.async = a
	return h
}

var validBackupTypes = map[string]bool{"full": true, "incr": true, "diff": true}

// validRestoreTypes includes "" (restore to latest).
var validRestoreTypes = map[string]bool{"": true, "time": true, "lsn": true, "xid": true, "name": true}

type createBackupRequest struct {
	Type string `json:"type"`
	// Annotation, when set, names the backup (stored as the "name" annotation on
	// the backup set and surfaced back in the catalog/info).
	Annotation string `json:"annotation"`
}

type backupPayload struct {
	ID          string            `json:"id"`
	Label       string            `json:"label"`
	Type        string            `json:"type"`
	RepoSize    int64             `json:"repo_size"`
	LogicalSize int64             `json:"logical_size"`
	WALStart    string            `json:"wal_start"`
	WALStop     string            `json:"wal_stop"`
	Error       bool              `json:"error"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Create starts a backup asynchronously, returning 202.
func (h *BackupsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createBackupRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if !validBackupTypes[req.Type] {
		respondError(w, apperr.New(apperr.KindInvalid, "backup type must be full, incr, or diff"))
		return
	}
	id := chi.URLParam(r, "id")
	opts := backup.RunOpts{Annotation: req.Annotation}
	recordAudit(h.audit, r, "backup.create", id)
	h.async.Go(func(ctx context.Context) { _ = h.runner.RunWith(ctx, id, req.Type, opts) })
	w.WriteHeader(http.StatusAccepted)
}

// List returns an instance's backup catalog.
func (h *BackupsHandler) List(w http.ResponseWriter, r *http.Request) {
	backups, err := h.runner.List(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	payloads := make([]backupPayload, 0, len(backups))
	for _, b := range backups {
		payloads = append(payloads, backupPayload{
			ID: b.ID, Label: b.Label, Type: b.Type, RepoSize: b.RepoSize,
			LogicalSize: b.LogicalSize, WALStart: b.WALStart, WALStop: b.WALStop, Error: b.Error,
			Annotations: b.Annotations,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": payloads})
}

// Delete removes a single backup set by label, synchronously. It runs
// pgbackrest expire --set against the instance's stanza and prunes the catalog
// row; on success it returns 204 No Content. The destructive nature of deleting
// a recovery point means it is gated at the backup-restore RBAC level (see the
// router), the most privileged backup action.
func (h *BackupsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	label := chi.URLParam(r, "label")
	if label == "" {
		respondError(w, apperr.New(apperr.KindInvalid, "backup label is required"))
		return
	}
	recordAudit(h.audit, r, "backup.delete", id+"/"+label)
	if err := h.runner.Delete(r.Context(), id, label); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Verify starts a repository integrity check (pgbackrest verify) asynchronously,
// returning 202. It is read-only on the repo (it never mutates backups), so it
// is gated at the backup-write level (see the router): it is an operational
// action a backup operator runs, not a privileged restore/destroy.
func (h *BackupsHandler) Verify(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recordAudit(h.audit, r, "backup.verify", id)
	h.async.Go(func(ctx context.Context) { _ = h.runner.Verify(ctx, id) })
	w.WriteHeader(http.StatusAccepted)
}

type restoreRequest struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Set    string `json:"set"`
	// Delta restores only changed files into the existing data dir (--delta).
	Delta bool `json:"delta"`
}

// Restore starts a restore (or PITR) asynchronously, returning 202.
func (h *BackupsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if !validRestoreTypes[req.Type] {
		respondError(w, apperr.New(apperr.KindInvalid, "restore type must be one of: time, lsn, xid, name"))
		return
	}
	if req.Type != "" && req.Target == "" {
		respondError(w, apperr.New(apperr.KindInvalid, "restore type requires a target"))
		return
	}
	id := chi.URLParam(r, "id")
	opts := provision.RestoreOptions{Type: req.Type, Target: req.Target, Set: req.Set, Delta: req.Delta}
	recordAudit(h.audit, r, "backup.restore", id)
	h.async.Go(func(ctx context.Context) { _ = h.restorer.Restore(ctx, id, opts, nil) })
	w.WriteHeader(http.StatusAccepted)
}
