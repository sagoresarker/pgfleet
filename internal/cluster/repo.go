package cluster

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

const uniqueViolation = "23505"

// Repository persists clusters.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a cluster Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const columns = `id, name, status, COALESCE(primary_instance_id::text, ''),
	router_container_id, router_port, last_error, pool_mode, created_at, updated_at`

// Create inserts a new cluster.
func (r *Repository) Create(ctx context.Context, in NewCluster) (Cluster, error) {
	if err := in.Validate(); err != nil {
		return Cluster{}, err
	}
	poolMode := in.PoolMode
	if poolMode == "" {
		poolMode = "transaction"
	}
	c, err := scan(r.pool.QueryRow(ctx,
		`INSERT INTO clusters (name, pool_mode) VALUES ($1, $2) RETURNING `+columns, in.Name, poolMode))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Cluster{}, apperr.Wrap(apperr.KindConflict, "cluster: name already exists", err)
		}
		return Cluster{}, apperr.Wrap(apperr.KindInternal, "cluster: create", err)
	}
	return c, nil
}

// Get returns a cluster by id.
func (r *Repository) Get(ctx context.Context, id string) (Cluster, error) {
	return r.one(ctx, `SELECT `+columns+` FROM clusters WHERE id = $1`, id)
}

// List returns all clusters, newest first.
func (r *Repository) List(ctx context.Context) ([]Cluster, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+columns+` FROM clusters ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "cluster: list", err)
	}
	defer rows.Close()
	var out []Cluster
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "cluster: scan", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetPrimary records the cluster's current primary instance. An empty id
// clears it (stored as SQL NULL).
func (r *Repository) SetPrimary(ctx context.Context, id, primaryInstanceID string) error {
	var primary any
	if primaryInstanceID != "" {
		primary = primaryInstanceID
	}
	return r.exec(ctx, `UPDATE clusters SET primary_instance_id = $2, updated_at = now() WHERE id = $1`,
		id, primary)
}

// SetRouter records the router container and host port.
func (r *Repository) SetRouter(ctx context.Context, id, containerID string, port int) error {
	return r.exec(ctx, `UPDATE clusters SET router_container_id = $2, router_port = $3, updated_at = now() WHERE id = $1`,
		id, containerID, port)
}

// SetStatus updates status and last_error.
func (r *Repository) SetStatus(ctx context.Context, id string, status Status, lastErr string) error {
	return r.exec(ctx, `UPDATE clusters SET status = $2, last_error = $3, updated_at = now() WHERE id = $1`,
		id, string(status), lastErr)
}

// Delete removes a cluster (cascades to its instances).
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.exec(ctx, `DELETE FROM clusters WHERE id = $1`, id)
}

func (r *Repository) one(ctx context.Context, query string, args ...any) (Cluster, error) {
	c, err := scan(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Cluster{}, apperr.New(apperr.KindNotFound, "cluster: not found")
	}
	if err != nil {
		return Cluster{}, apperr.Wrap(apperr.KindInternal, "cluster: query", err)
	}
	return c, nil
}

func (r *Repository) exec(ctx context.Context, query string, args ...any) error {
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "cluster: exec", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.KindNotFound, "cluster: not found")
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(row rowScanner) (Cluster, error) {
	var c Cluster
	err := row.Scan(&c.ID, &c.Name, &c.Status, &c.PrimaryInstanceID,
		&c.RouterContainerID, &c.RouterPort, &c.LastError, &c.PoolMode, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
