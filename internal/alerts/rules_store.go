package alerts

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// RuleStore persists user-configurable alert rules.
type RuleStore struct {
	pool *pgxpool.Pool
}

// NewRuleStore builds a RuleStore backed by the given pool.
func NewRuleStore(pool *pgxpool.Pool) *RuleStore {
	return &RuleStore{pool: pool}
}

const ruleColumns = `id, instance_id, kind, threshold, severity, enabled, created_at, updated_at`

// Create validates and inserts a rule, returning the persisted row (with its
// generated id and timestamps).
func (s *RuleStore) Create(ctx context.Context, r Rule) (Rule, error) {
	if err := r.Validate(); err != nil {
		return Rule{}, err
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO alert_rules (instance_id, kind, threshold, severity, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+ruleColumns,
		r.InstanceID, r.Kind, r.Threshold, r.Severity, r.Enabled)
	out, err := scanRule(row)
	if err != nil {
		return Rule{}, apperr.Wrap(apperr.KindInternal, "alert rules: create", err)
	}
	return out, nil
}

// List returns every rule, newest first.
func (s *RuleStore) List(ctx context.Context) ([]Rule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+ruleColumns+` FROM alert_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alert rules: list", err)
	}
	return collectRules(rows)
}

// Get returns one rule by id, or an apperr.KindNotFound error.
func (s *RuleStore) Get(ctx context.Context, id string) (Rule, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+ruleColumns+` FROM alert_rules WHERE id = $1`, id)
	out, err := scanRule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Rule{}, apperr.New(apperr.KindNotFound, "alert rule not found")
		}
		return Rule{}, apperr.Wrap(apperr.KindInternal, "alert rules: get", err)
	}
	return out, nil
}

// Update validates and overwrites a rule's mutable fields by id. It returns an
// apperr.KindNotFound error when no row matches.
func (s *RuleStore) Update(ctx context.Context, r Rule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE alert_rules
		    SET instance_id = $2, kind = $3, threshold = $4, severity = $5,
		        enabled = $6, updated_at = now()
		  WHERE id = $1`,
		r.ID, r.InstanceID, r.Kind, r.Threshold, r.Severity, r.Enabled)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "alert rules: update", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.KindNotFound, "alert rule not found")
	}
	return nil
}

// Delete removes a rule by id, returning an apperr.KindNotFound error when no
// row matches.
func (s *RuleStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "alert rules: delete", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.KindNotFound, "alert rule not found")
	}
	return nil
}

// ListEffective returns the enabled rules that apply to instanceID: rules with a
// NULL instance_id (apply to all instances) plus rules scoped to instanceID.
func (s *RuleStore) ListEffective(ctx context.Context, instanceID string) ([]Rule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+ruleColumns+`
		   FROM alert_rules
		  WHERE enabled = true AND (instance_id IS NULL OR instance_id = $1)
		  -- Global rules first, instance-scoped last, so that with last-wins merge
		  -- in EffectiveThresholds a specific rule ALWAYS beats a global one for the
		  -- same kind, regardless of which was created first.
		  ORDER BY (instance_id IS NULL) DESC, created_at ASC`, instanceID)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alert rules: list effective", err)
	}
	return collectRules(rows)
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanRule(row scanner) (Rule, error) {
	var r Rule
	if err := row.Scan(&r.ID, &r.InstanceID, &r.Kind, &r.Threshold, &r.Severity,
		&r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return Rule{}, err
	}
	return r, nil
}

func collectRules(rows pgx.Rows) ([]Rule, error) {
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "alert rules: scan", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "alert rules: rows", err)
	}
	return out, nil
}
