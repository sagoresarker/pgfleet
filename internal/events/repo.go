package events

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagoresarker/pgfleet/internal/apperr"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

// Store persists and queries durable events.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Filter narrows a List query. Each string field is optional; an empty value
// matches any. Limit defaults to 100 when <= 0 and is capped at 500.
type Filter struct {
	InstanceID string
	ClusterID  string
	Type       string
	Limit      int
}

// Record validates and persists a new event, returning the stored row.
func (s *Store) Record(ctx context.Context, ne NewEvent) (Event, error) {
	if err := ne.Validate(); err != nil {
		return Event{}, err
	}

	metadata := ne.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return Event{}, apperr.Wrap(apperr.KindInternal, "events: marshal metadata", err)
	}

	var instanceID, clusterID any
	if strings.TrimSpace(ne.InstanceID) != "" {
		instanceID = ne.InstanceID
	}
	if strings.TrimSpace(ne.ClusterID) != "" {
		clusterID = ne.ClusterID
	}

	row := s.pool.QueryRow(ctx,
		`INSERT INTO events (instance_id, cluster_id, type, message, metadata)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, COALESCE(instance_id::text, ''), COALESCE(cluster_id::text, ''),
		           type, message, metadata, created_at`,
		instanceID, clusterID, ne.Type, ne.Message, raw,
	)

	ev, err := scanEvent(row)
	if err != nil {
		return Event{}, apperr.Wrap(apperr.KindInternal, "events: record", err)
	}
	return ev, nil
}

// List returns events matching the filter, newest first (created_at DESC,
// id DESC), up to the (defaulted/capped) limit.
func (s *Store) List(ctx context.Context, f Filter) ([]Event, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	var (
		conds []string
		args  []any
	)
	add := func(col, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		args = append(args, val)
		conds = append(conds, col+" = $"+strconv.Itoa(len(args)))
	}
	add("instance_id", f.InstanceID)
	add("cluster_id", f.ClusterID)
	add("type", f.Type)

	q := `SELECT id, COALESCE(instance_id::text, ''), COALESCE(cluster_id::text, ''),
	             type, message, metadata, created_at
	      FROM events`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)
	q += " ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "events: list", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "events: scan", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "events: rows", err)
	}
	return events, nil
}

// scanner abstracts pgx.Row and pgx.Rows for a single-row scan.
type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(s scanner) (Event, error) {
	var (
		ev  Event
		raw []byte
	)
	if err := s.Scan(&ev.ID, &ev.InstanceID, &ev.ClusterID, &ev.Type, &ev.Message, &raw, &ev.CreatedAt); err != nil {
		return Event{}, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &ev.Metadata); err != nil {
			return Event{}, err
		}
	}
	if ev.Metadata == nil {
		ev.Metadata = map[string]string{}
	}
	return ev, nil
}
