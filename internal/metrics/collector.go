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
	addF := func(metric string, value float64) {
		s = append(s, Sample{InstanceID: instanceID, Metric: metric, Value: value, At: at})
	}

	// Database-wide activity and cumulative counters.
	var conns, active, dbSize, xactCommit, xactRollback, blksHit, blksRead, tupIns, tupUpd, tupDel, deadlocks int64
	var tupReturned, tupFetched, tempFiles, tempBytes, blkReadTime, blkWriteTime int64
	err = conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM pg_stat_activity WHERE datname IS NOT NULL),
			(SELECT count(*) FROM pg_stat_activity WHERE state = 'active'),
			pg_database_size(current_database()),
			d.xact_commit, d.xact_rollback, d.blks_hit, d.blks_read,
			d.tup_inserted, d.tup_updated, d.tup_deleted, d.deadlocks,
			d.tup_returned, d.tup_fetched, d.temp_files, d.temp_bytes,
			d.blk_read_time::bigint, d.blk_write_time::bigint
		FROM pg_stat_database d WHERE d.datname = current_database()`,
	).Scan(&conns, &active, &dbSize, &xactCommit, &xactRollback, &blksHit, &blksRead,
		&tupIns, &tupUpd, &tupDel, &deadlocks,
		&tupReturned, &tupFetched, &tempFiles, &tempBytes, &blkReadTime, &blkWriteTime)
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
	add("tup_returned", tupReturned)
	add("tup_fetched", tupFetched)
	add("temp_files", tempFiles)
	add("temp_bytes", tempBytes)
	add("blk_read_time_ms", blkReadTime)
	add("blk_write_time_ms", blkWriteTime)
	// Buffer cache hit ratio (%): the single most-watched health gauge.
	if total := blksHit + blksRead; total > 0 {
		addF("cache_hit_ratio", float64(blksHit)/float64(total)*100)
	}

	// Connection saturation and problem sessions.
	var maxConns, idleInTx, waiting int64
	var longestTxSecs float64
	if err := conn.QueryRow(ctx, `
		SELECT
			current_setting('max_connections')::bigint,
			(SELECT count(*) FROM pg_stat_activity WHERE state = 'idle in transaction'),
			(SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'),
			COALESCE(EXTRACT(EPOCH FROM (now() - min(xact_start))), 0)
		FROM pg_stat_activity WHERE xact_start IS NOT NULL`,
	).Scan(&maxConns, &idleInTx, &waiting, &longestTxSecs); err == nil {
		add("max_connections", maxConns)
		add("idle_in_transaction", idleInTx)
		add("waiting_sessions", waiting)
		addF("longest_transaction_seconds", longestTxSecs)
		if maxConns > 0 {
			addF("connection_utilization", float64(conns)/float64(maxConns)*100)
		}
	}

	// Lock pressure.
	var locksTotal, locksWaiting int64
	if err := conn.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE NOT granted) FROM pg_locks`,
	).Scan(&locksTotal, &locksWaiting); err == nil {
		add("locks_held", locksTotal)
		add("locks_waiting", locksWaiting)
	}

	// Checkpoint activity. PG17 moved checkpoint counters from pg_stat_bgwriter
	// to pg_stat_checkpointer; try the new view first, then fall back.
	var ckptTimed, ckptReq, buffersCkpt int64
	if err := conn.QueryRow(ctx,
		`SELECT num_timed, num_requested, buffers_written FROM pg_stat_checkpointer`,
	).Scan(&ckptTimed, &ckptReq, &buffersCkpt); err == nil {
		add("checkpoints_timed", ckptTimed)
		add("checkpoints_req", ckptReq)
		add("buffers_checkpoint", buffersCkpt)
	} else if err := conn.QueryRow(ctx,
		`SELECT checkpoints_timed, checkpoints_req, buffers_checkpoint FROM pg_stat_bgwriter`,
	).Scan(&ckptTimed, &ckptReq, &buffersCkpt); err == nil {
		add("checkpoints_timed", ckptTimed)
		add("checkpoints_req", ckptReq)
		add("buffers_checkpoint", buffersCkpt)
	}

	// WAL volume and generation.
	var walBytes, walRecords, walFPI int64
	if err := conn.QueryRow(ctx,
		`SELECT COALESCE(wal_bytes,0)::bigint, COALESCE(wal_records,0), COALESCE(wal_fpi,0) FROM pg_stat_wal`,
	).Scan(&walBytes, &walRecords, &walFPI); err == nil {
		add("wal_bytes", walBytes)
		add("wal_records", walRecords)
		add("wal_full_page_images", walFPI)
	}

	// Replication: lag on a standby, or the worst standby lag on a primary.
	c.collectReplication(ctx, conn, addF)

	return s, nil
}

// collectReplication adds replication-lag metrics. On a standby it reports how
// far behind the primary it is; on a primary it reports the worst replay lag
// across connected standbys and the number of connected replicas.
func (c *Collector) collectReplication(ctx context.Context, conn *pgx.Conn, addF func(string, float64)) {
	var inRecovery bool
	if err := conn.QueryRow(ctx, `SELECT pg_is_in_recovery()`).Scan(&inRecovery); err != nil {
		return
	}
	if inRecovery {
		var lagSecs float64
		if err := conn.QueryRow(ctx,
			`SELECT COALESCE(EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())), 0)`,
		).Scan(&lagSecs); err == nil {
			addF("replication_lag_seconds", lagSecs)
		}
		return
	}
	var standbys int64
	var worstLagBytes float64
	if err := conn.QueryRow(ctx, `
		SELECT count(*),
		       COALESCE(MAX(pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn)), 0)
		FROM pg_stat_replication`,
	).Scan(&standbys, &worstLagBytes); err == nil {
		addF("connected_standbys", float64(standbys))
		addF("replica_lag_bytes", worstLagBytes)
	}
}
