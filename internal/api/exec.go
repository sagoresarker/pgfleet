package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// execTimeout bounds how long a single exec may run so a never-exiting command
// (e.g. `tail -f`, `sleep infinity`) cannot pin a worker indefinitely.
const execTimeout = 60 * time.Second

// maxExecOutputBytes caps the stdout and stderr we echo back (each, separately).
// The runtime layer also bounds capture, but we clip here too so an oversized
// buffer is never serialized wholesale into the HTTP response.
const maxExecOutputBytes = 1 << 20 // 1 MiB

type execInstanceLookup interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

type execRuntime interface {
	Exec(ctx context.Context, id string, cmd []string) (docker.ExecResult, error)
}

// ExecHandler runs a one-shot command inside an instance's container (admin
// tooling — e.g. psql, file inspection). It is NOT an interactive shell.
type ExecHandler struct {
	instances execInstanceLookup
	rt        execRuntime
	audit     AuditRecorder
}

// NewExecHandler builds an ExecHandler.
func NewExecHandler(instances execInstanceLookup, rt execRuntime) *ExecHandler {
	return &ExecHandler{instances: instances, rt: rt}
}

// WithAudit attaches an audit recorder so each exec invocation is logged.
func (h *ExecHandler) WithAudit(rec AuditRecorder) *ExecHandler {
	h.audit = rec
	return h
}

type execRequest struct {
	// Command is argv (no shell). To use a shell, pass ["bash","-c","..."].
	Command []string `json:"command"`
}

// Run executes the command in the instance container and returns its output.
func (h *ExecHandler) Run(w http.ResponseWriter, r *http.Request) {
	var req execRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if len(req.Command) == 0 {
		respondError(w, apperr.New(apperr.KindInvalid, "command is required"))
		return
	}

	inst, err := h.instances.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if inst.ContainerID == "" {
		respondError(w, apperr.New(apperr.KindConflict, "instance has no running container"))
		return
	}

	// Audit the privileged attempt (arbitrary container command) now that the
	// request has passed its guards, before running it.
	recordAudit(h.audit, r, "instance.exec", inst.ID)

	// Bound the exec so a command that never exits cannot hang a worker.
	ctx, cancel := context.WithTimeout(r.Context(), execTimeout)
	defer cancel()

	res, err := h.rt.Exec(ctx, inst.ContainerID, req.Command)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "exec", err))
		return
	}

	// Clip stdout/stderr so a command emitting gigabytes is not serialized
	// wholesale into the response. The runtime also bounds capture upstream;
	// this is defense-in-depth and lets us flag truncation to the client.
	stdout, outTrunc := clipOutput(res.Stdout)
	stderr, errTrunc := clipOutput(res.Stderr)
	writeJSON(w, http.StatusOK, map[string]any{
		"exit_code": res.ExitCode,
		"stdout":    stdout,
		"stderr":    stderr,
		"truncated": outTrunc || errTrunc,
	})
}

// clipOutput truncates s to maxExecOutputBytes, returning the (possibly
// shortened) string and whether it was truncated. It clips on a byte boundary;
// callers treat output as opaque bytes, so a split multibyte rune is acceptable.
func clipOutput(s string) (string, bool) {
	if len(s) <= maxExecOutputBytes {
		return s, false
	}
	return s[:maxExecOutputBytes], true
}
