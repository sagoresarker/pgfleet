package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// dumpTimeout bounds a single logical dump.
const dumpTimeout = 30 * time.Minute

// DumpHandler streams a logical pg_dump of an instance so operators can download
// a portable backup. The control-plane image ships pg_dump.
type DumpHandler struct {
	instances execInstanceLookup
	dsn       sqlDSNResolver
	log       *slog.Logger
	audit     AuditRecorder
	// buildCmd constructs the dump command; overridable in tests. Defaults to
	// buildDumpCmd (real pg_dump).
	buildCmd func(ctx context.Context, dsn string) (*dumpCmd, error)
}

// NewDumpHandler builds a DumpHandler.
func NewDumpHandler(instances execInstanceLookup, dsn sqlDSNResolver) *DumpHandler {
	return &DumpHandler{
		instances: instances,
		dsn:       dsn,
		log:       slog.Default(),
		buildCmd: func(ctx context.Context, dsn string) (*dumpCmd, error) {
			cmd, err := buildDumpCmd(ctx, dsn)
			if err != nil {
				return nil, err
			}
			return newDumpCmd(cmd), nil
		},
	}
}

// WithLogger sets the logger used to report mid-stream pg_dump failures.
func (h *DumpHandler) WithLogger(l *slog.Logger) *DumpHandler {
	if l != nil {
		h.log = l
	}
	return h
}

// WithAudit attaches an audit recorder so each dump invocation is logged.
func (h *DumpHandler) WithAudit(rec AuditRecorder) *DumpHandler {
	h.audit = rec
	return h
}

// Get streams a plain-format pg_dump as a downloadable .sql file. The DSN
// password is passed via PGPASSWORD (not argv) so it is not visible in `ps`.
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

	// Audit the privileged attempt (full logical export of the database) before
	// streaming it.
	recordAudit(h.audit, r, "instance.dump", id)

	ctx, cancel := context.WithTimeout(r.Context(), dumpTimeout)
	defer cancel()

	dc, err := h.buildCmd(ctx, dsn)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "dump: build command", err))
		return
	}
	stdout, err := dc.cmd.StdoutPipe()
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "dump: pipe", err))
		return
	}
	if err := dc.cmd.Start(); err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "dump: start pg_dump (is it installed?)", err))
		return
	}

	// Copy a first chunk BEFORE committing headers, so an immediate failure
	// (e.g. auth/connection error before any data) can still be returned as a
	// proper error status instead of a silently-truncated 200.
	var first [4096]byte
	n, rerr := io.ReadFull(stdout, first[:])
	if n == 0 && rerr != nil {
		// Nothing was produced — the dump failed before its first byte. Drain
		// stderr and return a real error response (headers not yet sent).
		_ = dc.cmd.Wait()
		stderr := dc.stderr.String()
		h.log.Error("pg_dump produced no output", "instance", id, "stderr", stderr)
		respondError(w, apperr.New(apperr.KindInternal, "dump: pg_dump failed"))
		return
	}

	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", `attachment; filename="`+inst.Name+`.sql"`)
	w.WriteHeader(http.StatusOK)
	// Headers are committed now; from here a failure can only be logged, not
	// turned into an HTTP status.
	_, _ = w.Write(first[:n])
	_, _ = io.Copy(w, stdout)

	if werr := dc.cmd.Wait(); werr != nil {
		// SEC-7/REG-4: do not silently swallow a mid-stream failure. We cannot
		// change the already-sent 200, but we MUST surface the stderr so the
		// truncated download is explainable in the logs.
		h.log.Error("pg_dump failed mid-stream (download is truncated)",
			"instance", id, "err", werr, "stderr", dc.stderr.String())
	}
}

// dumpCmd bundles an exec.Cmd with the stderr buffer used to capture diagnostics
// on failure.
type dumpCmd struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
}

// newDumpCmd wires a stderr capture buffer onto cmd and returns the wrapper.
func newDumpCmd(cmd *exec.Cmd) *dumpCmd {
	var buf bytes.Buffer
	cmd.Stderr = &buf
	return &dumpCmd{cmd: cmd, stderr: &buf}
}

// newShellDumpCmd builds a dumpCmd that runs `sh -c script` — used by tests to
// simulate pg_dump success/failure without a real database.
func newShellDumpCmd(ctx context.Context, script string) *dumpCmd {
	return newDumpCmd(exec.CommandContext(ctx, "sh", "-c", script))
}

// buildDumpCmd parses the DSN and constructs a pg_dump command whose argv carries
// only non-secret connection params; the password is supplied via PGPASSWORD in
// the command environment so it is not exposed in `ps` (SEC-6/REG-3).
func buildDumpCmd(ctx context.Context, dsn string) (*exec.Cmd, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "dump: parse dsn", err)
	}

	args := []string{"--format=plain", "--no-owner", "--no-privileges"}
	if cfg.Host != "" {
		args = append(args, "--host="+cfg.Host)
	}
	if cfg.Port != 0 {
		args = append(args, "--port="+strconv.Itoa(int(cfg.Port)))
	}
	if cfg.User != "" {
		args = append(args, "--username="+cfg.User)
	}
	args = append(args, "--no-password")
	if cfg.Database != "" {
		args = append(args, "--dbname="+cfg.Database)
	}

	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	// Inherit the parent environment, then add PGPASSWORD so it never appears in
	// the process argv. Only set it when a password exists.
	cmd.Env = os.Environ()
	if cfg.Password != "" {
		cmd.Env = append(cmd.Env, "PGPASSWORD="+cfg.Password)
	}
	return cmd, nil
}
