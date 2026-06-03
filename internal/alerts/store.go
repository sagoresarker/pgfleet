package alerts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Transition records a change in an alert's state for one (instance, kind),
// so a notifier can fire only when something actually changes.
type Transition struct {
	Kind       string
	InstanceID string
	From       string // previous state ("" when there was no prior alert)
	To         string // new state: firing or resolved
	State      string // convenience mirror of To
	Severity   string
	Message    string
	Timestamp  time.Time
}

// Store persists alerts and reconciles them against evaluated findings.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds an alerts Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Sync reconciles the firing findings for one instance against the persisted
// alerts: it upserts an alert for each firing finding and resolves any firing
// alert whose kind is no longer in the finding set. It returns the transitions
// that actually changed state.
func (s *Store) Sync(ctx context.Context, instanceID string, findings []Finding) ([]Transition, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alerts: begin", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	var transitions []Transition
	firingKinds := make(map[string]struct{}, len(findings))

	for _, f := range findings {
		firingKinds[f.Kind] = struct{}{}

		// Was there already a firing alert for this (instance, kind)?
		var existed bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(
			   SELECT 1 FROM alerts
			   WHERE instance_id = $1 AND kind = $2 AND state = 'firing')`,
			instanceID, f.Kind,
		).Scan(&existed); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "alerts: sync exists", err)
		}

		value := f.Value
		threshold := f.Threshold
		// Upsert: insert a new firing alert, or refresh the existing one. The
		// partial unique index on (instance_id, kind) WHERE firing makes this a
		// safe upsert for active alerts.
		_, err := tx.Exec(ctx,
			`INSERT INTO alerts
			   (instance_id, kind, severity, state, message, value, threshold, fired_at, updated_at)
			 VALUES ($1, $2, $3, 'firing', $4, $5, $6, $7, $7)
			 ON CONFLICT (instance_id, kind) WHERE state = 'firing'
			 DO UPDATE SET severity = EXCLUDED.severity,
			               message   = EXCLUDED.message,
			               value     = EXCLUDED.value,
			               threshold = EXCLUDED.threshold,
			               updated_at = EXCLUDED.updated_at`,
			instanceID, f.Kind, f.Severity, f.Message, value, threshold, now,
		)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "alerts: sync upsert", err)
		}

		if !existed {
			transitions = append(transitions, Transition{
				Kind:       f.Kind,
				InstanceID: instanceID,
				From:       "",
				To:         StateFiring,
				State:      StateFiring,
				Severity:   f.Severity,
				Message:    f.Message,
				Timestamp:  now,
			})
		}
	}

	// Resolve any currently firing alert whose kind is no longer firing.
	rows, err := tx.Query(ctx,
		`SELECT kind, severity, message FROM alerts
		 WHERE instance_id = $1 AND state = 'firing'`, instanceID)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alerts: sync list firing", err)
	}
	type firing struct{ kind, severity, message string }
	var stale []firing
	for rows.Next() {
		var f firing
		if err := rows.Scan(&f.kind, &f.severity, &f.message); err != nil {
			rows.Close()
			return nil, apperr.Wrap(apperr.KindInternal, "alerts: sync scan firing", err)
		}
		if _, ok := firingKinds[f.kind]; !ok {
			stale = append(stale, f)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alerts: sync rows", err)
	}

	for _, f := range stale {
		_, err := tx.Exec(ctx,
			`UPDATE alerts SET state = 'resolved', resolved_at = $1, updated_at = $1
			 WHERE instance_id = $2 AND kind = $3 AND state = 'firing'`,
			now, instanceID, f.kind)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "alerts: sync resolve", err)
		}
		transitions = append(transitions, Transition{
			Kind:       f.kind,
			InstanceID: instanceID,
			From:       StateFiring,
			To:         StateResolved,
			State:      StateResolved,
			Severity:   f.severity,
			Message:    f.message,
			Timestamp:  now,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alerts: commit", err)
	}
	return transitions, nil
}

// ListActive returns all firing alerts, newest first.
func (s *Store) ListActive(ctx context.Context) ([]Alert, error) {
	return s.query(ctx,
		`SELECT id, instance_id, kind, severity, state, message, value, threshold,
		        fired_at, resolved_at, updated_at
		 FROM alerts WHERE state = 'firing'
		 ORDER BY fired_at DESC`)
}

// ListForInstance returns all alerts (firing and resolved) for one instance,
// newest first.
func (s *Store) ListForInstance(ctx context.Context, instanceID string) ([]Alert, error) {
	return s.query(ctx,
		`SELECT id, instance_id, kind, severity, state, message, value, threshold,
		        fired_at, resolved_at, updated_at
		 FROM alerts WHERE instance_id = $1
		 ORDER BY fired_at DESC`, instanceID)
}

func (s *Store) query(ctx context.Context, sql string, args ...any) ([]Alert, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alerts: list", err)
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		var a Alert
		var instanceID *string
		if err := rows.Scan(&a.ID, &instanceID, &a.Kind, &a.Severity, &a.State,
			&a.Message, &a.Value, &a.Threshold, &a.FiredAt, &a.ResolvedAt, &a.UpdatedAt); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "alerts: scan", err)
		}
		if instanceID != nil {
			a.InstanceID = *instanceID
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alerts: rows", err)
	}
	return out, nil
}
