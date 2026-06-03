package backup

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

// Catalog persists the backup catalog in the meta database.
type Catalog struct {
	pool *pgxpool.Pool
}

// NewCatalog builds a Catalog.
func NewCatalog(pool *pgxpool.Pool) *Catalog {
	return &Catalog{pool: pool}
}

// Upsert inserts or updates a backup row keyed by (instance_id, label).
func (c *Catalog) Upsert(ctx context.Context, instanceID string, b pgbackrest.BackupInfo) error {
	_, err := c.pool.Exec(ctx,
		`INSERT INTO backups
		 (instance_id, label, type, repo_size, logical_size, wal_start, wal_stop, started_at, stopped_at, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (instance_id, label) DO UPDATE SET
		   type = EXCLUDED.type,
		   repo_size = EXCLUDED.repo_size,
		   logical_size = EXCLUDED.logical_size,
		   wal_start = EXCLUDED.wal_start,
		   wal_stop = EXCLUDED.wal_stop,
		   started_at = EXCLUDED.started_at,
		   stopped_at = EXCLUDED.stopped_at,
		   error = EXCLUDED.error`,
		instanceID, b.Label, b.Type, b.RepoSize, b.Size, b.WALStart, b.WALStop,
		b.StartTime, b.StopTime, b.Error,
	)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "backup: upsert", err)
	}
	return nil
}

// Prune removes catalog rows for an instance whose labels are not in keep
// (i.e. backups that pgBackRest has expired). An empty keep set removes all.
func (c *Catalog) Prune(ctx context.Context, instanceID string, keep []string) error {
	_, err := c.pool.Exec(ctx,
		`DELETE FROM backups WHERE instance_id = $1 AND NOT (label = ANY($2))`,
		instanceID, keep,
	)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "backup: prune", err)
	}
	return nil
}

// Delete removes a single catalog row for an instance by label (the backup the
// operator deleted via pgbackrest expire --set). It is a no-op if the label is
// absent, so callers can treat re-deletes idempotently.
func (c *Catalog) Delete(ctx context.Context, instanceID, label string) error {
	_, err := c.pool.Exec(ctx,
		`DELETE FROM backups WHERE instance_id = $1 AND label = $2`,
		instanceID, label,
	)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "backup: delete", err)
	}
	return nil
}

// List returns an instance's backups, newest first.
func (c *Catalog) List(ctx context.Context, instanceID string) ([]Backup, error) {
	rows, err := c.pool.Query(ctx,
		`SELECT id, instance_id, label, type, repo_size, logical_size, wal_start, wal_stop,
		        started_at, stopped_at, error
		 FROM backups WHERE instance_id = $1
		 ORDER BY stopped_at DESC NULLS LAST, label DESC`, instanceID)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "backup: list", err)
	}
	defer rows.Close()

	var out []Backup
	for rows.Next() {
		var b Backup
		if err := rows.Scan(&b.ID, &b.InstanceID, &b.Label, &b.Type, &b.RepoSize, &b.LogicalSize,
			&b.WALStart, &b.WALStop, &b.StartedAt, &b.StoppedAt, &b.Error); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "backup: scan", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
