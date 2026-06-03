package remotebackup

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/objectstore"
	"github.com/sagoresarker/pgfleet/internal/secrets"
)

// Repository persists the remote-dump catalog in the control-plane meta
// database and, optionally, the sealed connection secret for a captured source
// (so a re-capture does not require re-entering credentials). It satisfies
// Catalog.
type Repository struct {
	pool   *pgxpool.Pool
	cipher *secrets.Cipher
}

// NewRepository builds a remote-dump Repository. cipher seals/unseals stored
// source passwords; it must be non-nil if SaveSource is used.
func NewRepository(pool *pgxpool.Pool, cipher *secrets.Cipher) *Repository {
	return &Repository{pool: pool, cipher: cipher}
}

const dumpColumns = `id, object_key, source_host, source_db, server_major, size_bytes, created_at`

// Save inserts a catalog row and returns it with its generated id. The source
// host stored is expected to already be redacted (the Service redacts it).
func (r *Repository) Save(ctx context.Context, e CatalogEntry) (CatalogEntry, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO remote_dumps (object_key, source_host, source_db, server_major, size_bytes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+dumpColumns,
		e.ObjectKey, e.SourceHost, e.SourceDB, e.ServerMaj, e.Size)
	return scanDump(row)
}

// Get returns one catalog entry by id.
func (r *Repository) Get(ctx context.Context, id string) (CatalogEntry, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+dumpColumns+` FROM remote_dumps WHERE id = $1`, id)
	e, err := scanDump(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogEntry{}, apperr.New(apperr.KindNotFound, "remotebackup: dump not found")
	}
	return e, err
}

// List returns all catalog entries, newest first.
func (r *Repository) List(ctx context.Context) ([]CatalogEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+dumpColumns+` FROM remote_dumps ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "remotebackup: list dumps", err)
	}
	defer rows.Close()
	var out []CatalogEntry
	for rows.Next() {
		e, serr := scanDump(rows)
		if serr != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "remotebackup: scan dump", serr)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveSource seals and persists the connection secret for a captured dump so it
// can be re-captured later without re-entering the password. The password is
// encrypted with the secrets cipher (NEVER stored plaintext).
func (r *Repository) SaveSource(ctx context.Context, dumpID string, c RemoteConn) error {
	if r.cipher == nil {
		return apperr.New(apperr.KindInternal, "remotebackup: no cipher configured")
	}
	c.applyDefaults()
	sealed, err := r.cipher.Encrypt([]byte(c.Password))
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "remotebackup: seal source password", err)
	}
	blob, err := secrets.Marshal(sealed)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "remotebackup: marshal sealed secret", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO remote_source_secrets (dump_id, host, port, db_user, dbname, sslmode, password_secret)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (dump_id) DO UPDATE SET
		   host = EXCLUDED.host, port = EXCLUDED.port, db_user = EXCLUDED.db_user,
		   dbname = EXCLUDED.dbname, sslmode = EXCLUDED.sslmode,
		   password_secret = EXCLUDED.password_secret`,
		dumpID, c.Host, c.Port, c.User, c.DBName, c.SSLMode, blob)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "remotebackup: save source secret", err)
	}
	return nil
}

// Source unseals and returns the stored connection details for a captured dump.
func (r *Repository) Source(ctx context.Context, dumpID string) (RemoteConn, error) {
	if r.cipher == nil {
		return RemoteConn{}, apperr.New(apperr.KindInternal, "remotebackup: no cipher configured")
	}
	var c RemoteConn
	var blob []byte
	err := r.pool.QueryRow(ctx,
		`SELECT host, port, db_user, dbname, sslmode, password_secret
		 FROM remote_source_secrets WHERE dump_id = $1`, dumpID).
		Scan(&c.Host, &c.Port, &c.User, &c.DBName, &c.SSLMode, &blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return RemoteConn{}, apperr.New(apperr.KindNotFound, "remotebackup: source not found")
	}
	if err != nil {
		return RemoteConn{}, apperr.Wrap(apperr.KindInternal, "remotebackup: read source", err)
	}
	sealed, err := secrets.Unmarshal(blob)
	if err != nil {
		return RemoteConn{}, apperr.Wrap(apperr.KindInternal, "remotebackup: unmarshal source secret", err)
	}
	plain, err := r.cipher.Decrypt(sealed)
	if err != nil {
		return RemoteConn{}, apperr.Wrap(apperr.KindInternal, "remotebackup: decrypt source secret", err)
	}
	c.Password = string(plain)
	return c, nil
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDump(s rowScanner) (CatalogEntry, error) {
	var e CatalogEntry
	if err := s.Scan(&e.ID, &e.ObjectKey, &e.SourceHost, &e.SourceDB, &e.ServerMaj, &e.Size, &e.CreatedAt); err != nil {
		return CatalogEntry{}, err
	}
	return e, nil
}

// ObjectStoreAdapter adapts internal/objectstore (a value-config API) to the
// ObjectStore interface this package consumes.
type ObjectStoreAdapter struct {
	Cfg objectstore.Config
}

// NewObjectStore builds an ObjectStore backed by the given object-store config.
func NewObjectStore(cfg objectstore.Config) *ObjectStoreAdapter {
	return &ObjectStoreAdapter{Cfg: cfg}
}

// Put writes data under key.
func (a *ObjectStoreAdapter) Put(ctx context.Context, key string, data []byte) error {
	return objectstore.PutObject(ctx, a.Cfg, key, data)
}

// Get reads the object under key.
func (a *ObjectStoreAdapter) Get(ctx context.Context, key string) ([]byte, error) {
	return objectstore.GetObject(ctx, a.Cfg, key)
}
