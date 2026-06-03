package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// maxSQLRows bounds how many rows the console returns.
const maxSQLRows = 1000

// maxSQLBytes bounds the total size (in bytes) of the cell values the console
// buffers and returns. A row-count cap alone is insufficient: a single 1-row ×
// 1GB result (e.g. a large bytea/text column) would OOM the control plane. We
// stop collecting once the accumulated cell bytes exceed this budget and mark
// the result truncated.
const maxSQLBytes = 8 << 20 // 8 MiB

// sqlDSNResolver resolves an instance's connection string.
type sqlDSNResolver func(ctx context.Context, instanceID string) (string, error)

// SQLHandler runs ad-hoc SQL from the dashboard against a managed instance.
type SQLHandler struct {
	dsn   sqlDSNResolver
	audit AuditRecorder
}

// NewSQLHandler builds a SQLHandler.
func NewSQLHandler(dsn sqlDSNResolver) *SQLHandler {
	return &SQLHandler{dsn: dsn}
}

// WithAudit attaches an audit recorder so each SQL invocation is logged.
func (h *SQLHandler) WithAudit(rec AuditRecorder) *SQLHandler {
	h.audit = rec
	return h
}

type sqlRequest struct {
	Query string `json:"query"`
}

// Run executes the query and returns columns + rows (or the rows affected for a
// non-SELECT). SQL errors are surfaced to the client as 400s so the console can
// show them.
func (h *SQLHandler) Run(w http.ResponseWriter, r *http.Request) {
	var req sqlRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if req.Query == "" {
		respondError(w, apperr.New(apperr.KindInvalid, "query is required"))
		return
	}

	id := chi.URLParam(r, "id")
	dsn, err := h.dsn(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	// Audit the privileged attempt (superuser SQL) before executing it, so the
	// action is recorded even if the query later errors.
	recordAudit(h.audit, r, "instance.sql", id)

	// Bound the whole operation so a runaway query can't pin a worker.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "sql: connect", err))
		return
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, req.Query)
	if err != nil {
		// A SQL error is the user's problem, not ours — surface the message.
		respondError(w, apperr.Wrap(apperr.KindInvalid, "query failed", err))
		return
	}
	defer rows.Close()

	cols, out, truncated, cerr := collectRows(rows)
	if cerr != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "sql: read row", cerr))
		return
	}
	if err := rows.Err(); err != nil {
		respondError(w, apperr.Wrap(apperr.KindInvalid, "query failed", err))
		return
	}
	tag := rows.CommandTag()

	writeJSON(w, http.StatusOK, map[string]any{
		"columns":       cols,
		"rows":          out,
		"rows_affected": tag.RowsAffected(),
		"command":       tag.String(),
		"truncated":     truncated,
	})
}

// sqlRows is the slice of pgx.Rows that collectRows consumes. It is an
// interface so the collection logic can be unit-tested with an in-memory fake.
type sqlRows interface {
	FieldDescriptions() []pgconn.FieldDescription
	Next() bool
	Values() ([]any, error)
}

// collectRows reads result rows into memory, bounded by BOTH a row-count cap
// (maxSQLRows) and a total-bytes budget (maxSQLBytes). It returns the column
// names, the collected rows, and whether collection stopped early (truncated).
// The byte budget defends against a small number of very large rows OOMing the
// control plane — a row-count cap alone cannot. The caller is still responsible
// for checking rows.Err() after this returns.
func collectRows(rows sqlRows) (cols []string, out [][]any, truncated bool, err error) {
	for _, fd := range rows.FieldDescriptions() {
		cols = append(cols, string(fd.Name))
	}
	out = make([][]any, 0)
	var total int64
	for rows.Next() {
		if len(out) >= maxSQLRows {
			truncated = true
			break
		}
		vals, verr := rows.Values()
		if verr != nil {
			return cols, out, truncated, verr
		}
		for _, v := range vals {
			total += cellBytes(v)
		}
		out = append(out, vals)
		// Stop AFTER appending the row that tips us over, so the client always
		// gets at least one row even if it alone exceeds the budget (and so a
		// caller can tell the result was clipped via truncated).
		if total > maxSQLBytes {
			truncated = true
			break
		}
	}
	return cols, out, truncated, nil
}

// cellBytes estimates the in-memory size of a single decoded cell value. It
// covers the heavy cases (strings, byte slices) exactly and falls back to a
// stringified length for everything else, which is a reasonable proxy for the
// budget's purpose (bounding total buffered bytes).
func cellBytes(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case string:
		return int64(len(t))
	case []byte:
		return int64(len(t))
	default:
		return int64(len(fmt.Sprintf("%v", t)))
	}
}
