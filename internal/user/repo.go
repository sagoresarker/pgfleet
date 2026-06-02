package user

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagoresarker/pgfleet/internal/apperr"
)

const uniqueViolation = "23505"

// Repository persists users in the meta database.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository builds a user Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new user. The email is normalized; a duplicate email
// (case-insensitive) returns a KindConflict error.
func (r *Repository) Create(ctx context.Context, in NewUser) (User, error) {
	if err := in.Validate(); err != nil {
		return User{}, err
	}
	email := NormalizeEmail(in.Email)

	var u User
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, role)
		 VALUES ($1, $2, $3)
		 RETURNING id, email, password_hash, role, disabled, created_at, updated_at`,
		email, in.PasswordHash, string(in.Role),
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return User{}, apperr.Wrap(apperr.KindConflict, "user: email already exists", err)
		}
		return User{}, apperr.Wrap(apperr.KindInternal, "user: create", err)
	}
	return u, nil
}

// GetByEmail looks up a user by normalized email.
func (r *Repository) GetByEmail(ctx context.Context, email string) (User, error) {
	return r.scanOne(ctx,
		`SELECT id, email, password_hash, role, disabled, created_at, updated_at
		 FROM users WHERE lower(email) = lower($1)`,
		NormalizeEmail(email),
	)
}

// GetByID looks up a user by id.
func (r *Repository) GetByID(ctx context.Context, id string) (User, error) {
	return r.scanOne(ctx,
		`SELECT id, email, password_hash, role, disabled, created_at, updated_at
		 FROM users WHERE id = $1`,
		id,
	)
}

// List returns all users ordered by creation time.
func (r *Repository) List(ctx context.Context) ([]User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, email, password_hash, role, disabled, created_at, updated_at
		 FROM users ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "user: list", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "user: scan", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// SetDisabled enables or disables a user.
func (r *Repository) SetDisabled(ctx context.Context, id string, disabled bool) error {
	return r.exec(ctx,
		`UPDATE users SET disabled = $2, updated_at = now() WHERE id = $1`,
		id, disabled)
}

// UpdatePasswordHash replaces a user's password hash (e.g. after rehash).
func (r *Repository) UpdatePasswordHash(ctx context.Context, id, hash string) error {
	return r.exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`,
		id, hash)
}

func (r *Repository) scanOne(ctx context.Context, query string, args ...any) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx, query, args...).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, apperr.New(apperr.KindNotFound, "user: not found")
	}
	if err != nil {
		return User{}, apperr.Wrap(apperr.KindInternal, "user: query", err)
	}
	return u, nil
}

func (r *Repository) exec(ctx context.Context, query string, args ...any) error {
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "user: exec", err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.New(apperr.KindNotFound, "user: not found")
	}
	return nil
}
