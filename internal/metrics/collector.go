package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Collector connects to a managed instance and reads PostgreSQL statistics.
type Collector struct {
	now func() time.Time
}

// NewCollector builds a Collector.
func NewCollector() *Collector {
	return &Collector{now: time.Now}
}

// Collect connects to the instance at dsn, reads stats, and returns samples
// stamped with the collection time. The connection is closed before returning.
func (c *Collector) Collect(ctx context.Context, instanceID, dsn string) ([]Sample, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "metrics: connect", err)
	}
	defer conn.Close(ctx)

	at := c.now()
	var s []Sample
	add := func(metric string, value int64) {
		s = append(s, Sample{InstanceID: instanceID, Metric: metric, Value: float64(value), At: at})
	}

	// Database-wide activity and cumulative counters.
	var conns, active, dbSize, xactCommit, xactRollback, blksHit, blksRead, tupIns, tupUpd, tupDel, deadlocks int64
	err = conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM pg_stat_activity WHERE datname IS NOT NULL),
			(SELECT count(*) FROM pg_stat_activity WHERE state = 'active'),
			pg_database_size(current_database()),
			d.xact_commit, d.xact_rollback, d.blks_hit, d.blks_read,
			d.tup_inserted, d.tup_updated, d.tup_deleted, d.deadlocks
		FROM pg_stat_database d WHERE d.datname = current_database()`,
	).Scan(&conns, &active, &dbSize, &xactCommit, &xactRollback, &blksHit, &blksRead,
		&tupIns, &tupUpd, &tupDel, &deadlocks)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "metrics: query stat_database", err)
	}
	add("connections", conns)
	add("active_connections", active)
	add("db_size_bytes", dbSize)
	add("xact_commit", xactCommit)
	add("xact_rollback", xactRollback)
	add("blks_hit", blksHit)
	add("blks_read", blksRead)
	add("tup_inserted", tupIns)
	add("tup_updated", tupUpd)
	add("tup_deleted", tupDel)
	add("deadlocks", deadlocks)

	// Checkpoint activity (PG16: pg_stat_bgwriter).
	var ckptTimed, ckptReq, buffersCkpt int64
	if err := conn.QueryRow(ctx,
		`SELECT checkpoints_timed, checkpoints_req, buffers_checkpoint FROM pg_stat_bgwriter`,
	).Scan(&ckptTimed, &ckptReq, &buffersCkpt); err == nil {
		add("checkpoints_timed", ckptTimed)
		add("checkpoints_req", ckptReq)
		add("buffers_checkpoint", buffersCkpt)
	}

	// WAL volume.
	var walBytes int64
	if err := conn.QueryRow(ctx, `SELECT COALESCE(wal_bytes, 0)::bigint FROM pg_stat_wal`).Scan(&walBytes); err == nil {
		add("wal_bytes", walBytes)
	}

	return s, nil
}
