// Package user models control-plane user accounts and their persistence.
package user

import (
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
)

// User is a control-plane account.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         auth.Role
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewUser is the input for creating a user.
type NewUser struct {
	Email        string
	PasswordHash string
	Role         auth.Role
}

// Validate checks required fields and the role.
func (n NewUser) Validate() error {
	if strings.TrimSpace(n.Email) == "" {
		return apperr.New(apperr.KindInvalid, "user: email is required")
	}
	if n.PasswordHash == "" {
		return apperr.New(apperr.KindInvalid, "user: password hash is required")
	}
	if !n.Role.Valid() {
		return apperr.New(apperr.KindInvalid, "user: invalid role")
	}
	return nil
}

// NormalizeEmail lower-cases and trims an email for case-insensitive storage
// and lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
