// Package metrics collects PostgreSQL statistics from managed instances and
// stores them as time series in the control-plane meta database.
package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Sample is a single metric observation.
type Sample struct {
	InstanceID string    `json:"-"`
	Metric     string    `json:"metric"`
	Value      float64   `json:"value"`
	At         time.Time `json:"at"`
}

// Store persists and queries metric samples in the meta database.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a metrics Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Insert writes a batch of samples.
func (s *Store) Insert(ctx context.Context, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	rows := make([][]any, len(samples))
	for i, sm := range samples {
		rows[i] = []any{sm.InstanceID, sm.Metric, sm.Value, sm.At}
	}
	_, err := s.pool.CopyFrom(ctx,
		pgx.Identifier{"metric_samples"},
		[]string{"instance_id", "metric", "value", "at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "metrics: insert", err)
	}
	return nil
}

// Query returns samples for one metric within a time range, oldest first.
func (s *Store) Query(ctx context.Context, instanceID, metric string, since, until time.Time) ([]Sample, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT metric, value, at FROM metric_samples
		 WHERE instance_id = $1 AND metric = $2 AND at >= $3 AND at <= $4
		 ORDER BY at ASC`,
		instanceID, metric, since, until)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "metrics: query", err)
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		s := Sample{InstanceID: instanceID}
		if err := rows.Scan(&s.Metric, &s.Value, &s.At); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "metrics: scan", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Latest returns the most recent sample per metric for an instance.
func (s *Store) Latest(ctx context.Context, instanceID string) (map[string]Sample, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (metric) metric, value, at
		 FROM metric_samples WHERE instance_id = $1
		 ORDER BY metric, at DESC`, instanceID)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "metrics: latest", err)
	}
	defer rows.Close()

	out := map[string]Sample{}
	for rows.Next() {
		s := Sample{InstanceID: instanceID}
		if err := rows.Scan(&s.Metric, &s.Value, &s.At); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "metrics: scan", err)
		}
		out[s.Metric] = s
	}
	return out, rows.Err()
}

// Prune deletes samples older than the cutoff (retention).
func (s *Store) Prune(ctx context.Context, before time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM metric_samples WHERE at < $1`, before)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "metrics: prune", err)
	}
	return nil
}
