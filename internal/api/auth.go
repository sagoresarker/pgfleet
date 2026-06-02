package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/audit"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

// UserAuthStore is the subset of the user repository the auth handler needs.
type UserAuthStore interface {
	GetByEmail(ctx context.Context, email string) (user.User, error)
	UpdatePasswordHash(ctx context.Context, id, hash string) error
}

// TokenIssuer mints session tokens.
type TokenIssuer interface {
	Issue(userID, email string, role auth.Role) (string, error)
}

// AuditRecorder records audit events (optional; may be nil).
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Entry) error
}

// AuthHandler serves login/logout.
type AuthHandler struct {
	users  UserAuthStore
	tokens TokenIssuer
	audit  AuditRecorder
}

// NewAuthHandler builds an AuthHandler. audit may be nil.
func NewAuthHandler(users UserAuthStore, tokens TokenIssuer, rec AuditRecorder) *AuthHandler {
	return &AuthHandler{users: users, tokens: tokens, audit: rec}
}

// dummyHash is a real argon2id hash used to spend equivalent work when an
// email is unknown, defeating login timing-based user enumeration.
var dummyHash, _ = auth.HashPassword("pgfleet-constant-time-placeholder")

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userPayload struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Login authenticates a user and returns a signed token plus user info.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, err)
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		respondError(w, apperr.New(apperr.KindInvalid, "email and password are required"))
		return
	}

	ctx := r.Context()
	u, err := h.users.GetByEmail(ctx, req.Email)
	if err != nil {
		// Perform a dummy verification so the response time does not reveal
		// whether the email exists (constant-work path), then return the same
		// generic error as a wrong password.
		_, _ = auth.VerifyPassword(req.Password, dummyHash)
		h.record(ctx, "auth.login.failed", req.Email)
		respondError(w, apperr.New(apperr.KindUnauthorized, "invalid credentials"))
		return
	}

	ok, err := auth.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil || !ok {
		h.record(ctx, "auth.login.failed", req.Email)
		respondError(w, apperr.New(apperr.KindUnauthorized, "invalid credentials"))
		return
	}

	// Reject disabled accounts only after a correct password, with a distinct
	// 403 (the operator must know their account is disabled, not deleted).
	if u.Disabled {
		h.record(ctx, "auth.login.disabled", u.Email)
		respondError(w, apperr.New(apperr.KindForbidden, "account is disabled"))
		return
	}

	// Opportunistically upgrade a weak hash now that we have the plaintext.
	if auth.NeedsRehash(u.PasswordHash) {
		if newHash, herr := auth.HashPassword(req.Password); herr == nil {
			_ = h.users.UpdatePasswordHash(ctx, u.ID, newHash)
		}
	}

	token, err := h.tokens.Issue(u.ID, u.Email, u.Role)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "issue token", err))
		return
	}

	h.record(ctx, "auth.login", u.Email)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  userPayload{ID: u.ID, Email: u.Email, Role: string(u.Role)},
	})
}

// Logout is a client-driven endpoint for stateless tokens: the client discards
// the token. Tokens expire via their TTL.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, ok := auth.ClaimsFromContext(r.Context()); ok {
		h.record(r.Context(), "auth.logout", c.Email)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) record(ctx context.Context, action, target string) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Record(ctx, audit.Entry{Actor: target, Action: action, Target: target})
}
