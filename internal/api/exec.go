package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

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
}

// NewExecHandler builds an ExecHandler.
func NewExecHandler(instances execInstanceLookup, rt execRuntime) *ExecHandler {
	return &ExecHandler{instances: instances, rt: rt}
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

	res, err := h.rt.Exec(r.Context(), inst.ContainerID, req.Command)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "exec", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exit_code": res.ExitCode,
		"stdout":    res.Stdout,
		"stderr":    res.Stderr,
	})
}
