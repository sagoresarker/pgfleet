package auth

import "github.com/sagoresarker/pgfleet/internal/apperr"

// Role is a control-plane access role. The set is fixed and single-org.
type Role string

const (
	// RoleAdmin can manage users and perform any operation.
	RoleAdmin Role = "admin"
	// RoleOperator can manage instances and backups but not users.
	RoleOperator Role = "operator"
	// RoleViewer has read-only access.
	RoleViewer Role = "viewer"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

// ParseRole validates and converts a string to a Role.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", apperr.New(apperr.KindInvalid, "invalid role: "+s)
	}
	return r, nil
}
