package timescale

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Conn is the minimal subset of *pgx.Conn the Service needs to execute
// TimescaleDB management statements and read information views. *pgx.Conn
// satisfies it, so the API layer can pass a live connection directly. The
// signatures mirror pgx/v5 exactly.
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Service manages TimescaleDB objects on a managed instance. It is stateless:
// the caller supplies a Conn per call (one connection to the target instance).
type Service struct{}

// NewService builds a Service.
func NewService() *Service { return &Service{} }

// exec builds-then-executes a single statement, wrapping errors. build is one of
// the sql.go builders; its returned KindInvalid error (on bad input) is passed
// through untouched, while execution failures are wrapped as KindInternal.
func exec(ctx context.Context, conn Conn, op string, sql string, buildErr error) error {
	if buildErr != nil {
		return buildErr
	}
	if _, err := conn.Exec(ctx, sql); err != nil {
		return apperr.Wrap(apperr.KindInternal, "timescale: "+op, err)
	}
	return nil
}

// CreateHypertable converts table into a hypertable partitioned by timeColumn.
func (s *Service) CreateHypertable(ctx context.Context, conn Conn, table, timeColumn string) error {
	sql, err := CreateHypertableSQL(table, timeColumn)
	return exec(ctx, conn, "create_hypertable", sql, err)
}

// AddRetentionPolicy adds a retention policy dropping chunks older than dropAfter.
func (s *Service) AddRetentionPolicy(ctx context.Context, conn Conn, hypertable, dropAfter string) error {
	sql, err := AddRetentionPolicySQL(hypertable, dropAfter)
	return exec(ctx, conn, "add_retention_policy", sql, err)
}

// RemoveRetentionPolicy removes the retention policy from hypertable.
func (s *Service) RemoveRetentionPolicy(ctx context.Context, conn Conn, hypertable string) error {
	sql, err := RemoveRetentionPolicySQL(hypertable)
	return exec(ctx, conn, "remove_retention_policy", sql, err)
}

// EnableCompression enables columnar compression on hypertable.
func (s *Service) EnableCompression(ctx context.Context, conn Conn, hypertable, segmentBy, orderBy string) error {
	sql, err := EnableCompressionSQL(hypertable, segmentBy, orderBy)
	return exec(ctx, conn, "enable_compression", sql, err)
}

// AddCompressionPolicy adds a policy compressing chunks older than compressAfter.
func (s *Service) AddCompressionPolicy(ctx context.Context, conn Conn, hypertable, compressAfter string) error {
	sql, err := AddCompressionPolicySQL(hypertable, compressAfter)
	return exec(ctx, conn, "add_compression_policy", sql, err)
}

// RemoveCompressionPolicy removes the compression policy from hypertable.
func (s *Service) RemoveCompressionPolicy(ctx context.Context, conn Conn, hypertable string) error {
	sql, err := RemoveCompressionPolicySQL(hypertable)
	return exec(ctx, conn, "remove_compression_policy", sql, err)
}

// CreateContinuousAggregate creates a continuous aggregate named name from query.
func (s *Service) CreateContinuousAggregate(ctx context.Context, conn Conn, name, query string) error {
	sql, err := CreateContinuousAggregateSQL(name, query)
	return exec(ctx, conn, "create_continuous_aggregate", sql, err)
}

// AddContinuousAggregatePolicy adds a refresh policy to the continuous aggregate name.
func (s *Service) AddContinuousAggregatePolicy(ctx context.Context, conn Conn, name, startOffset, endOffset, scheduleInterval string) error {
	sql, err := AddContinuousAggregatePolicySQL(name, startOffset, endOffset, scheduleInterval)
	return exec(ctx, conn, "add_continuous_aggregate_policy", sql, err)
}

// ListHypertables returns every hypertable on the instance with its chunk count,
// on-disk size, and whether compression is enabled.
func (s *Service) ListHypertables(ctx context.Context, conn Conn) ([]Hypertable, error) {
	rows, err := conn.Query(ctx, `
		SELECT
			h.hypertable_schema,
			h.hypertable_name,
			COALESCE(c.num_chunks, 0),
			COALESCE(hypertable_size(format('%I.%I', h.hypertable_schema, h.hypertable_name)), 0),
			h.compression_enabled
		FROM timescaledb_information.hypertables h
		LEFT JOIN (
			SELECT hypertable_schema, hypertable_name, count(*) AS num_chunks
			FROM timescaledb_information.chunks
			GROUP BY hypertable_schema, hypertable_name
		) c ON c.hypertable_schema = h.hypertable_schema
		   AND c.hypertable_name = h.hypertable_name
		ORDER BY h.hypertable_schema, h.hypertable_name`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "timescale: list hypertables", err)
	}
	defer rows.Close()

	var out []Hypertable
	for rows.Next() {
		var h Hypertable
		if err := rows.Scan(&h.Schema, &h.Name, &h.NumChunks, &h.SizeBytes, &h.CompressionEnabled); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "timescale: scan hypertable", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "timescale: list hypertables", err)
	}
	return out, nil
}

// ListJobs returns the TimescaleDB background jobs (policy schedules) on the
// instance.
func (s *Service) ListJobs(ctx context.Context, conn Conn) ([]Job, error) {
	rows, err := conn.Query(ctx, `
		SELECT
			j.job_id,
			COALESCE(j.application_name, ''),
			COALESCE(j.schedule_interval::text, ''),
			s.next_start,
			COALESCE(s.last_run_status, '')
		FROM timescaledb_information.jobs j
		LEFT JOIN timescaledb_information.job_stats s ON s.job_id = j.job_id
		ORDER BY j.job_id`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "timescale: list jobs", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Application, &j.ScheduleInterval, &j.NextStart, &j.LastRunStatus); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "timescale: scan job", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "timescale: list jobs", err)
	}
	return out, nil
}
