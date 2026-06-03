package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/remotebackup"
)

// RemoteService is the subset of *remotebackup.Service the handler needs. It is
// an interface so handler tests can substitute a fake without a real pg_dump on
// PATH or an object store.
type RemoteService interface {
	Capture(ctx context.Context, c remotebackup.RemoteConn) (remotebackup.CatalogEntry, error)
	List(ctx context.Context) ([]remotebackup.CatalogEntry, error)
	GetEntry(ctx context.Context, id string) (remotebackup.CatalogEntry, error)
	RestoreInto(ctx context.Context, dumpID, targetDSN string) error
}

// RemoteTargetProvisioner provisions a fresh PgFleet target (a single instance
// or a primary+replica cluster) for a migrate-in restore, waits until it is
// ready to accept connections, returns its superuser DSN, and can mark it
// errored if the restore fails. Implemented by an adapter over the existing
// instance/cluster provisioning in main.go wiring; faked in tests.
//
// The two ProvisionInstance/ProvisionCluster methods return the new target's id
// AND a DSN that already carries the fresh superuser password (so the restore
// never has to re-derive a secret). WaitReady blocks until the target can be
// restored into, or returns an error (timeout / provisioning failure). MarkError
// records that the restore failed so the operator does not see a half-built
// target reported as healthy.
type RemoteTargetProvisioner interface {
	// ProvisionInstance creates+provisions a single instance and returns its id.
	ProvisionInstance(ctx context.Context, spec RemoteTargetSpec) (id string, err error)
	// ProvisionCluster creates+provisions a primary+replica cluster and returns
	// its id.
	ProvisionCluster(ctx context.Context, spec RemoteTargetSpec) (id string, err error)
	// WaitReady blocks until the target (instance or cluster) is ready and
	// returns the superuser DSN to restore into.
	WaitReady(ctx context.Context, kind, id string) (dsn string, err error)
	// MarkError records that the migrate-in restore failed for the target so it
	// is not left silently half-built.
	MarkError(ctx context.Context, kind, id, reason string)
}

// RemoteTargetSpec is the provisioning spec for a restore target.
type RemoteTargetSpec struct {
	Name       string
	Password   string
	RepoType   string
	PGVersion  string
	Replicas   int
	Parameters map[string]string
	Extensions []string
}

// RemoteHandler serves the migrate-in (remote backup & restore) endpoints.
type RemoteHandler struct {
	svc   RemoteService
	prov  RemoteTargetProvisioner
	audit AuditRecorder
	async *Async
	// restoreTimeout bounds the background provision+wait+restore; overridable
	// in tests so they do not depend on the production-length wall clock.
	restoreTimeout time.Duration
}

// NewRemoteHandler builds a RemoteHandler. prov is required for the restore
// endpoint; if nil, restore returns 503.
func NewRemoteHandler(svc RemoteService, prov RemoteTargetProvisioner) *RemoteHandler {
	return &RemoteHandler{svc: svc, prov: prov, restoreTimeout: 90 * time.Minute}
}

// WithAudit attaches an audit recorder.
func (h *RemoteHandler) WithAudit(rec AuditRecorder) *RemoteHandler {
	h.audit = rec
	return h
}

// WithAsync attaches the background-task tracker so the provision+restore is
// drained on graceful shutdown.
func (h *RemoteHandler) WithAsync(a *Async) *RemoteHandler {
	h.async = a
	return h
}

// captureRequest is the POST /remote/backups body. Password is write-only and
// is NEVER echoed back.
type captureRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

// remoteDumpPayload is the password-free catalog representation returned to
// clients. SourceHost is already redacted by the service.
type remoteDumpPayload struct {
	ID          string `json:"id"`
	ObjectKey   string `json:"object_key"`
	SourceHost  string `json:"source_host"`
	SourceDB    string `json:"source_db"`
	ServerMajor int    `json:"server_major"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

func toRemoteDumpPayload(e remotebackup.CatalogEntry) remoteDumpPayload {
	return remoteDumpPayload{
		ID:          e.ID,
		ObjectKey:   e.ObjectKey,
		SourceHost:  e.SourceHost,
		SourceDB:    e.SourceDB,
		ServerMajor: e.ServerMaj,
		SizeBytes:   e.Size,
		CreatedAt:   e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// Capture connects to the remote Postgres, captures a custom-format pg_dump into
// the object store, catalogs it, and returns the catalog entry. The password is
// never echoed and is redacted from every error.
//
// This is synchronous: the operator wants to know immediately whether the
// supplied credentials work and whether the dump succeeded. The service bounds
// the operation with its own capture timeout.
func (h *RemoteHandler) Capture(w http.ResponseWriter, r *http.Request) {
	var req captureRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	conn := remotebackup.RemoteConn{
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: req.Password,
		DBName:   req.DBName,
		SSLMode:  req.SSLMode,
	}
	// Validate up front so an obviously-bad request 400s before we audit/work.
	if err := conn.Validate(); err != nil {
		respondError(w, err)
		return
	}

	entry, err := h.svc.Capture(r.Context(), conn)
	if err != nil {
		respondError(w, err)
		return
	}
	// Audit with the REDACTED host only — never the raw host/user/password.
	recordAudit(h.audit, r, "remote.backup.capture", entry.SourceHost+"/"+entry.SourceDB)
	writeJSON(w, http.StatusCreated, map[string]any{"backup": toRemoteDumpPayload(entry)})
}

// List returns all captured remote dumps (newest first), password-free.
func (h *RemoteHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	payloads := make([]remoteDumpPayload, 0, len(items))
	for _, e := range items {
		payloads = append(payloads, toRemoteDumpPayload(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": payloads})
}

// remoteRestoreRequest is the POST /remote/backups/{id}/restore body.
type remoteRestoreRequest struct {
	Target     string            `json:"target"` // "instance" | "cluster"
	Name       string            `json:"name"`
	Password   string            `json:"password"`
	RepoType   string            `json:"repo_type"`
	PGVersion  string            `json:"pg_version"`
	Replicas   int               `json:"replicas"`
	Parameters map[string]string `json:"parameters"`
	Extensions []string          `json:"extensions"`
}

const (
	targetInstance = "instance"
	targetCluster  = "cluster"
)

// Restore provisions a fresh target (single instance or primary+replica cluster)
// and, once it is ready, loads the captured dump into it. The provision+restore
// runs in a tracked background goroutine; the response is 202 Accepted with the
// kind+id of the new target so the dashboard can poll its status. On restore
// failure the target is marked errored (never left silently half-built).
func (h *RemoteHandler) Restore(w http.ResponseWriter, r *http.Request) {
	if h.prov == nil {
		// A missing provisioner is a server-side misconfiguration, not a client
		// error; surface it as 500 (respondError scrubs the message for 5xx).
		respondError(w, apperr.New(apperr.KindInternal, "remote restore is not configured"))
		return
	}
	dumpID := chi.URLParam(r, "id")

	var req remoteRestoreRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if req.Target != targetInstance && req.Target != targetCluster {
		respondError(w, apperr.New(apperr.KindInvalid, `target must be "instance" or "cluster"`))
		return
	}
	if len(req.Password) < 8 {
		respondError(w, apperr.New(apperr.KindInvalid, "password must be at least 8 characters"))
		return
	}
	if req.Target == targetCluster && req.Replicas < 1 {
		respondError(w, apperr.New(apperr.KindInvalid, "a cluster restore requires at least 1 replica"))
		return
	}
	// Confirm the dump exists before we provision anything (a bad id must 404,
	// not provision then fail).
	if _, err := h.svc.GetEntry(r.Context(), dumpID); err != nil {
		respondError(w, err)
		return
	}

	spec := RemoteTargetSpec{
		Name:       req.Name,
		Password:   req.Password,
		RepoType:   req.RepoType,
		PGVersion:  req.PGVersion,
		Replicas:   req.Replicas,
		Parameters: req.Parameters,
		Extensions: req.Extensions,
	}

	// Provision synchronously enough to obtain the new target id (so we can
	// return it), then do the long wait+restore in the background.
	var (
		kind = req.Target
		id   string
		err  error
	)
	if kind == targetInstance {
		id, err = h.prov.ProvisionInstance(r.Context(), spec)
	} else {
		id, err = h.prov.ProvisionCluster(r.Context(), spec)
	}
	if err != nil {
		respondError(w, err)
		return
	}

	recordAudit(h.audit, r, "remote.backup.restore", kind+":"+id+" from dump "+dumpID)

	timeout := h.restoreTimeout
	h.async.Go(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		dsn, werr := h.prov.WaitReady(ctx, kind, id)
		if werr != nil {
			h.prov.MarkError(ctx, kind, id, "target never became ready: "+safeReason(werr))
			return
		}
		if rerr := h.svc.RestoreInto(ctx, dumpID, dsn); rerr != nil {
			h.prov.MarkError(ctx, kind, id, "restore failed: "+safeReason(rerr))
			return
		}
	})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"target": kind,
		"id":     id,
	})
}

// safeReason returns an error's message for recording against a target. The
// remotebackup service already redacts the remote/target password from its
// errors, so this is safe; we keep it small and explicit in case the reason is
// later surfaced to operators.
func safeReason(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
