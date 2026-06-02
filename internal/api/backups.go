package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// BackupRunner takes backups and lists the catalog.
type BackupRunner interface {
	Run(ctx context.Context, instanceID, backupType string) error
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
}

// NewBackupsHandler builds a BackupsHandler. audit may be nil.
func NewBackupsHandler(runner BackupRunner, restorer Restorer, rec AuditRecorder) *BackupsHandler {
	return &BackupsHandler{runner: runner, restorer: restorer, audit: rec}
}

var validBackupTypes = map[string]bool{"full": true, "incr": true, "diff": true}

type createBackupRequest struct {
	Type string `json:"type"`
}

type backupPayload struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	RepoSize    int64  `json:"repo_size"`
	LogicalSize int64  `json:"logical_size"`
	WALStart    string `json:"wal_start"`
	WALStop     string `json:"wal_stop"`
	Error       bool   `json:"error"`
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
	recordAudit(h.audit, r, "backup.create", id)
	go func() { _ = h.runner.Run(context.Background(), id, req.Type) }()
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
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": payloads})
}

type restoreRequest struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Set    string `json:"set"`
}

// Restore starts a restore (or PITR) asynchronously, returning 202.
func (h *BackupsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if req.Type != "" && req.Target == "" {
		respondError(w, apperr.New(apperr.KindInvalid, "restore type requires a target"))
		return
	}
	id := chi.URLParam(r, "id")
	opts := provision.RestoreOptions{Type: req.Type, Target: req.Target, Set: req.Set}
	recordAudit(h.audit, r, "backup.restore", id)
	go func() { _ = h.restorer.Restore(context.Background(), id, opts, nil) }()
	w.WriteHeader(http.StatusAccepted)
}
