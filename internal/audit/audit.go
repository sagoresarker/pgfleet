// Package audit records an append-only log of mutating actions taken through
// the control plane (who did what to which target).
package audit

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Entry is a single audit record.
type Entry struct {
	ID        string
	Actor     string         // user identifier or "system"
	Action    string         // dotted verb, e.g. "instance.create"
	Target    string         // optional subject of the action
	Metadata  map[string]any // optional structured context
	CreatedAt time.Time
}

// Validate checks the required fields of an Entry.
func (e Entry) Validate() error {
	if strings.TrimSpace(e.Actor) == "" {
		return apperr.New(apperr.KindInvalid, "audit: actor is required")
	}
	if strings.TrimSpace(e.Action) == "" {
		return apperr.New(apperr.KindInvalid, "audit: action is required")
	}
	return nil
}

// Recorder persists and queries audit entries.
type Recorder struct {
	pool *pgxpool.Pool
}

// NewRecorder builds a Recorder backed by the given pool.
func NewRecorder(pool *pgxpool.Pool) *Recorder {
	return &Recorder{pool: pool}
}

// Record validates and persists an audit entry.
func (r *Recorder) Record(ctx context.Context, e Entry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	metadata := e.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO audit_log (actor, action, target, metadata)
		 VALUES ($1, $2, $3, $4)`,
		e.Actor, e.Action, e.Target, metadata,
	)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "audit: record", err)
	}
	return nil
}

// List returns the most recent entries, newest first, up to limit.
func (r *Recorder) List(ctx context.Context, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, actor, action, target, metadata, created_at
		 FROM audit_log
		 ORDER BY created_at DESC, id DESC
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "audit: list", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "audit: scan", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "audit: rows", err)
	}
	return entries, nil
}
