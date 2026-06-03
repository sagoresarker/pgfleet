package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// DumpHandler streams a logical pg_dump of an instance so operators can download
// a portable backup. The control-plane image ships pg_dump.
type DumpHandler struct {
	instances execInstanceLookup
	dsn       sqlDSNResolver
}

// NewDumpHandler builds a DumpHandler.
func NewDumpHandler(instances execInstanceLookup, dsn sqlDSNResolver) *DumpHandler {
	return &DumpHandler{instances: instances, dsn: dsn}
}

// Get streams `pg_dump <dsn>` as a downloadable .sql file.
func (h *DumpHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inst, err := h.instances.Get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	dsn, err := h.dsn(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pg_dump", "--format=plain", "--no-owner", "--no-privileges", dsn)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "dump: pipe", err))
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "dump: start pg_dump (is it installed?)", err))
		return
	}

	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", `attachment; filename="`+inst.Name+`.sql"`)
	// Headers are committed once we copy, so errors after this point can only be
	// logged/truncated, not turned into an HTTP status.
	_, _ = io.Copy(w, stdout)
	_ = cmd.Wait()
}
