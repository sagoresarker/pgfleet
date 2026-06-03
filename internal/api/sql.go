package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// maxSQLRows bounds how many rows the console returns.
const maxSQLRows = 1000

// sqlDSNResolver resolves an instance's connection string.
type sqlDSNResolver func(ctx context.Context, instanceID string) (string, error)

// SQLHandler runs ad-hoc SQL from the dashboard against a managed instance.
type SQLHandler struct {
	dsn sqlDSNResolver
}

// NewSQLHandler builds a SQLHandler.
func NewSQLHandler(dsn sqlDSNResolver) *SQLHandler {
	return &SQLHandler{dsn: dsn}
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

	dsn, err := h.dsn(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}

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

	cols := make([]string, 0)
	for _, fd := range rows.FieldDescriptions() {
		cols = append(cols, string(fd.Name))
	}

	out := make([][]any, 0)
	truncated := false
	for rows.Next() {
		if len(out) >= maxSQLRows {
			truncated = true
			break
		}
		vals, verr := rows.Values()
		if verr != nil {
			respondError(w, apperr.Wrap(apperr.KindInternal, "sql: read row", verr))
			return
		}
		out = append(out, vals)
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
