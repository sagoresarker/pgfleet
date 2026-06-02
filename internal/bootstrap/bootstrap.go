// Package bootstrap seeds initial control-plane state, such as the first
// admin user on a fresh database.
package bootstrap

import (
	"context"
	"fmt"

	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

// Store is the subset of the user repository needed to seed an admin.
type Store interface {
	List(ctx context.Context) ([]user.User, error)
	Create(ctx context.Context, in user.NewUser) (user.User, error)
}

// EnsureAdmin creates an admin account from the given credentials, but only on
// an empty user store. It is a no-op when credentials are missing or any user
// already exists, making it safe to call on every boot. It reports whether an
// admin was created.
func EnsureAdmin(ctx context.Context, store Store, email, password string) (bool, error) {
	if email == "" || password == "" {
		return false, nil
	}

	existing, err := store.List(ctx)
	if err != nil {
		return false, fmt.Errorf("bootstrap: list users: %w", err)
	}
	if len(existing) > 0 {
		return false, nil
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, fmt.Errorf("bootstrap: hash password: %w", err)
	}
	if _, err := store.Create(ctx, user.NewUser{
		Email: email, PasswordHash: hash, Role: auth.RoleAdmin,
	}); err != nil {
		return false, fmt.Errorf("bootstrap: create admin: %w", err)
	}
	return true, nil
}
