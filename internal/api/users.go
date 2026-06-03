package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

// minPasswordLen is the minimum acceptable password length for new accounts.
const minPasswordLen = 8

// UserAdminStore is the subset of the user repository the admin handler needs.
type UserAdminStore interface {
	Create(ctx context.Context, in user.NewUser) (user.User, error)
	List(ctx context.Context) ([]user.User, error)
	SetDisabled(ctx context.Context, id string, disabled bool) error
}

// UsersHandler serves admin user management.
type UsersHandler struct {
	store UserAdminStore
	audit AuditRecorder
}

// NewUsersHandler builds a UsersHandler. audit may be nil.
func NewUsersHandler(store UserAdminStore, rec AuditRecorder) *UsersHandler {
	return &UsersHandler{store: store, audit: rec}
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// Create provisions a new user with a hashed password.
func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if len(req.Password) < minPasswordLen {
		respondError(w, apperr.New(apperr.KindInvalid, "password must be at least 8 characters"))
		return
	}
	role, err := auth.ParseRole(req.Role)
	if err != nil {
		respondError(w, err)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "hash password", err))
		return
	}

	u, err := h.store.Create(r.Context(), user.NewUser{
		Email: req.Email, PasswordHash: hash, Role: role,
	})
	if err != nil {
		respondError(w, err)
		return
	}

	recordAudit(h.audit, r, "user.create", u.Email)
	writeJSON(w, http.StatusCreated, map[string]any{"user": toPayload(u)})
}

// List returns all users.
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	payloads := make([]userPayload, 0, len(users))
	for _, u := range users {
		payloads = append(payloads, toPayload(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": payloads})
}

// Disable disables a user account.
func (h *UsersHandler) Disable(w http.ResponseWriter, r *http.Request) {
	h.setDisabled(w, r, true, "user.disable")
}

// Enable re-enables a user account.
func (h *UsersHandler) Enable(w http.ResponseWriter, r *http.Request) {
	h.setDisabled(w, r, false, "user.enable")
}

func (h *UsersHandler) setDisabled(w http.ResponseWriter, r *http.Request, disabled bool, action string) {
	id := chi.URLParam(r, "id")
	if disabled {
		if err := h.guardDisable(r, id); err != nil {
			respondError(w, err)
			return
		}
	}
	if err := h.store.SetDisabled(r.Context(), id, disabled); err != nil {
		respondError(w, err)
		return
	}
	recordAudit(h.audit, r, action, id)
	w.WriteHeader(http.StatusNoContent)
}

// guardDisable refuses disable actions that would lock everyone out of the
// control plane. Login rejects disabled accounts, and there is no in-app
// recovery, so both of these would be unrecoverable:
//   - an admin disabling their OWN account, and
//   - disabling the LAST remaining active admin.
func (h *UsersHandler) guardDisable(r *http.Request, id string) error {
	if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims.UserID == id {
		return apperr.New(apperr.KindInvalid, "you cannot disable your own account")
	}
	users, err := h.store.List(r.Context())
	if err != nil {
		return err
	}
	var target *user.User
	activeAdmins := 0
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
		}
		if users[i].Role == auth.RoleAdmin && !users[i].Disabled {
			activeAdmins++
		}
	}
	// Unknown id: let SetDisabled produce the NotFound so behaviour is unchanged.
	if target == nil {
		return nil
	}
	if target.Role == auth.RoleAdmin && !target.Disabled && activeAdmins <= 1 {
		return apperr.New(apperr.KindInvalid, "cannot disable the last active admin")
	}
	return nil
}

func toPayload(u user.User) userPayload {
	return userPayload{ID: u.ID, Email: u.Email, Role: string(u.Role)}
}
