package health

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Store persists the latest health report per instance.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a health Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Upsert stores (or replaces) the report for an instance.
func (s *Store) Upsert(ctx context.Context, r Report) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO instance_health
		 (instance_id, archiving_ok, has_backup, last_backup_age_secs, wal_bytes, drill_ran, drill_ok, issues, checked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (instance_id) DO UPDATE SET
		   archiving_ok = EXCLUDED.archiving_ok,
		   has_backup = EXCLUDED.has_backup,
		   last_backup_age_secs = EXCLUDED.last_backup_age_secs,
		   wal_bytes = EXCLUDED.wal_bytes,
		   drill_ran = EXCLUDED.drill_ran,
		   drill_ok = EXCLUDED.drill_ok,
		   issues = EXCLUDED.issues,
		   checked_at = EXCLUDED.checked_at`,
		r.InstanceID, r.ArchivingOK, r.HasBackup, int64(r.LastBackupAge.Seconds()),
		r.WALBytes, r.DrillRan, r.DrillOK, r.Issues, r.CheckedAt,
	)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "health: upsert", err)
	}
	return nil
}

// UpdateDrill records a restore-drill outcome without clobbering the rest of
// the report (created by a health check).
func (s *Store) UpdateDrill(ctx context.Context, instanceID string, ok bool) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO instance_health (instance_id, drill_ran, drill_ok)
		 VALUES ($1, true, $2)
		 ON CONFLICT (instance_id) DO UPDATE SET
		   drill_ran = true, drill_ok = EXCLUDED.drill_ok`,
		instanceID, ok)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "health: update drill", err)
	}
	return nil
}

// List returns all stored health reports.
func (s *Store) List(ctx context.Context) ([]Report, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT instance_id, archiving_ok, has_backup, last_backup_age_secs, wal_bytes,
		        drill_ran, drill_ok, issues, checked_at
		 FROM instance_health`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "health: list", err)
	}
	defer rows.Close()

	var out []Report
	for rows.Next() {
		var r Report
		var ageSecs int64
		if err := rows.Scan(&r.InstanceID, &r.ArchivingOK, &r.HasBackup, &ageSecs, &r.WALBytes,
			&r.DrillRan, &r.DrillOK, &r.Issues, &r.CheckedAt); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "health: scan", err)
		}
		r.LastBackupAge = time.Duration(ageSecs) * time.Second
		out = append(out, r)
	}
	return out, rows.Err()
}
