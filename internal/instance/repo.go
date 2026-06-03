package instance

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/secrets"
)

// DefaultImage is the managed-instance image used when neither image nor
// version is specified. It must match docker.ManagedImage.
const DefaultImage = "pgfleet/postgres-pgbackrest:" + DefaultVersion

const uniqueViolation = "23505"

// Repository persists instances and transparently encrypts the superuser
// password at rest using the provided cipher.
type Repository struct {
	pool   *pgxpool.Pool
	cipher *secrets.Cipher
}

// NewRepository builds an instance Repository.
func NewRepository(pool *pgxpool.Pool, cipher *secrets.Cipher) *Repository {
	return &Repository{pool: pool, cipher: cipher}
}

const instanceColumns = `id, name, status, image, pg_version, container_id,
	host_port, data_volume, repo_type, stanza, superuser, last_error,
	COALESCE(cluster_id::text, ''), role, created_at, updated_at`

// Create provisions an instance row with an encrypted superuser password.
func (r *Repository) Create(ctx context.Context, in NewInstance) (Instance, error) {
	if err := in.Validate(); err != nil {
		return Instance{}, err
	}
	applyDefaults(&in)

	sealed, err := r.cipher.Encrypt([]byte(in.Password))
	if err != nil {
		return Instance{}, apperr.Wrap(apperr.KindInternal, "instance: encrypt password", err)
	}
	blob, err := secrets.Marshal(sealed)
	if err != nil {
		return Instance{}, apperr.Wrap(apperr.KindInternal, "instance: marshal secret", err)
	}

	var clusterID any
	if in.ClusterID != "" {
		clusterID = in.ClusterID
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO instances (name, image, pg_version, repo_type, stanza, superuser, superuser_secret, cluster_id, role)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+instanceColumns,
		in.Name, in.Image, in.PGVersion, string(in.RepoType),
		StanzaFor(in.Name), in.Superuser, blob, clusterID, string(in.Role),
	)
	inst, err := scanInstance(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Instance{}, apperr.Wrap(apperr.KindConflict, "instance: name already exists", err)
		}
		return Instance{}, apperr.Wrap(apperr.KindInternal, "instance: create", err)
	}
	return inst, nil
}

// Get returns an instance by id.
func (r *Repository) Get(ctx context.Context, id string) (Instance, error) {
	return r.queryOne(ctx, `SELECT `+instanceColumns+` FROM instances WHERE id = $1`, id)
}

// GetByName returns an instance by name.
func (r *Repository) GetByName(ctx context.Context, name string) (Instance, error) {
	return r.queryOne(ctx, `SELECT `+instanceColumns+` FROM instances WHERE name = $1`, name)
}

// List returns all instances, newest first.
func (r *Repository) List(ctx context.Context) ([]Instance, error) {
	return r.queryMany(ctx, `SELECT `+instanceColumns+` FROM instances ORDER BY created_at DESC, id DESC`)
}

// ListByCluster returns the instances belonging to a cluster, primary first.
func (r *Repository) ListByCluster(ctx context.Context, clusterID string) ([]Instance, error) {
	return r.queryMany(ctx,
		`SELECT `+instanceColumns+` FROM instances WHERE cluster_id = $1
		 ORDER BY (role = 'primary') DESC, created_at ASC`, clusterID)
}

// SetRole changes an instance's cluster role (e.g. on failover promotion).
func (r *Repository) SetRole(ctx context.Context, id string, role Role) error {
	return r.exec(ctx, `UPDATE instances SET role = $2, updated_at = now() WHERE id = $1`, id, string(role))
}

func (r *Repository) queryMany(ctx context.Context, query string, args ...any) ([]Instance, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "instance: query", err)
	}
	defer rows.Close()

	var out []Instance
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "instance: scan", err)
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// SetStatus updates status and last_error (pass "" to clear the error).
func (r *Repository) SetStatus(ctx context.Context, id string, status Status, lastErr string) error {
	return r.exec(ctx,
		`UPDATE instances SET status = $2, last_error = $3, updated_at = now() WHERE id = $1`,
		id, string(status), lastErr)
}

// SetRuntime records the container id and host port after provisioning.
func (r *Repository) SetRuntime(ctx context.Context, id, containerID string, hostPort int) error {
	return r.exec(ctx,
		`UPDATE instances SET container_id = $2, host_port = $3, updated_at = now() WHERE id = $1`,
		id, containerID, hostPort)
}

// SetDataVolume records the instance's current data volume (changes when a
// restore swaps onto a fresh volume).
func (r *Repository) SetDataVolume(ctx context.Context, id, volume string) error {
	return r.exec(ctx,
		`UPDATE instances SET data_volume = $2, updated_at = now() WHERE id = $1`,
		id, volume)
}

// Password decrypts and returns the superuser password for an instance.
func (r *Repository) Password(ctx context.Context, id string) (string, error) {
	var blob []byte
	err := r.pool.QueryRow(ctx, `SELECT superuser_secret FROM instances WHERE id = $1`, id).Scan(&blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperr.New(apperr.KindNotFound, "instance: not found")
	}
	if err != nil {
		return "", apperr.Wrap(apperr.KindInternal, "instance: read secret", err)
	}
	sealed, err := secrets.Unmarshal(blob)
	if err != nil {
		return "", apperr.Wrap(apperr.KindInternal, "instance: unmarshal secret", err)
	}
	plain, err := r.cipher.Decrypt(sealed)
	if err != nil {
		return "", apperr.Wrap(apperr.KindInternal, "instance: decrypt secret", err)
	}
	return string(plain), nil
}

// Delete removes an instance row.
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.exec(ctx, `DELETE FROM instances WHERE id = $1`, id)
}

func (r *Repository) queryOne(ctx context.Context, query string, args ...any) (Instance, error) {
	inst, err := scanInstance(r.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Instance{}, apperr.New(apperr.KindNotFound, "instance: not found")
	}
	if err != nil {
		return Instance{}, apperr.Wrap(apperr.KindInternal, "instance: query", err)
	}
	return inst, nil
}

func (r *Repository) exec(ctx context.Context, query string, args ...any) error {
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "instance: exec", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.KindNotFound, "instance: not found")
	}
	return nil
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanInstance(row rowScanner) (Instance, error) {
	var i Instance
	err := row.Scan(&i.ID, &i.Name, &i.Status, &i.Image, &i.PGVersion, &i.ContainerID,
		&i.HostPort, &i.DataVolume, &i.RepoType, &i.Stanza, &i.Superuser, &i.LastError,
		&i.ClusterID, &i.Role, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

func applyDefaults(in *NewInstance) {
	if in.PGVersion == "" {
		in.PGVersion = DefaultVersion
	}
	// Derive the image from the chosen version unless an explicit image was
	// given, so each version provisions from its matching pgBackRest image.
	if in.Image == "" {
		in.Image = ImageForVersion(in.PGVersion)
	}
	if in.Superuser == "" {
		in.Superuser = "postgres"
	}
	if in.Role == "" {
		in.Role = RoleStandalone
	}
}
