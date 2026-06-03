package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/audit"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

// SSOUserStore is the subset of the user repository the SSO handler needs.
type SSOUserStore interface {
	GetByEmail(ctx context.Context, email string) (user.User, error)
	Create(ctx context.Context, in user.NewUser) (user.User, error)
}

// SSOConfig configures trusted-header single sign-on. EmailHeader is the request
// header an upstream identity-aware proxy (Authelia behind a reverse proxy doing
// forward-auth, or any OIDC proxy) injects with the authenticated user's email.
//
// SECURITY: the handler TRUSTS this header unconditionally, so it is only mounted
// when EmailHeader is configured, and the operator MUST guarantee that:
//   - the reverse proxy strips any client-supplied copy of EmailHeader/GroupsHeader
//     and sets them only from the verified upstream identity, and
//   - the control-plane API is reachable ONLY through that proxy (not directly).
type SSOConfig struct {
	EmailHeader   string // e.g. "Remote-Email"; empty disables SSO
	GroupsHeader  string // e.g. "Remote-Groups"; optional, drives role mapping
	AutoProvision bool   // create a PgFleet user on first SSO login
	AdminGroup    string // group name that maps to the admin role
	OperatorGroup string // group name that maps to the operator role
}

// Enabled reports whether SSO is configured.
func (c SSOConfig) Enabled() bool { return strings.TrimSpace(c.EmailHeader) != "" }

func (c SSOConfig) adminGroup() string {
	if c.AdminGroup != "" {
		return c.AdminGroup
	}
	return "pgfleet-admins"
}

func (c SSOConfig) operatorGroup() string {
	if c.OperatorGroup != "" {
		return c.OperatorGroup
	}
	return "pgfleet-operators"
}

// SSOHandler exchanges a proxy-verified identity (trusted header) for a normal
// PgFleet session token, so an external IdP like Authelia can front the dashboard
// while PgFleet keeps its own users/roles/audit trail.
type SSOHandler struct {
	store  SSOUserStore
	tokens TokenIssuer
	audit  AuditRecorder
	cfg    SSOConfig
}

// NewSSOHandler builds an SSOHandler. audit may be nil.
func NewSSOHandler(store SSOUserStore, tokens TokenIssuer, cfg SSOConfig) *SSOHandler {
	return &SSOHandler{store: store, tokens: tokens, cfg: cfg}
}

// WithAudit attaches an audit recorder.
func (h *SSOHandler) WithAudit(rec AuditRecorder) *SSOHandler {
	h.audit = rec
	return h
}

// Exchange reads the trusted identity header and returns a PgFleet token + user
// (the same shape as password login) so the frontend can establish a session.
func (h *SSOHandler) Exchange(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.Header.Get(h.cfg.EmailHeader))
	if email == "" {
		// No upstream identity — the caller did not come through the IdP proxy.
		respondError(w, apperr.New(apperr.KindUnauthorized, "no single sign-on identity present"))
		return
	}

	ctx := r.Context()
	u, err := h.store.GetByEmail(ctx, email)
	if err != nil {
		if apperr.Kind(err) == apperr.KindNotFound && h.cfg.AutoProvision {
			u, err = h.provision(ctx, email, r.Header.Get(h.cfg.GroupsHeader))
		}
		if err != nil {
			if apperr.Kind(err) == apperr.KindNotFound {
				h.record(ctx, "auth.sso.unprovisioned", email)
				respondError(w, apperr.New(apperr.KindForbidden, "no PgFleet account is provisioned for this identity"))
				return
			}
			respondError(w, err)
			return
		}
	}

	if u.Disabled {
		h.record(ctx, "auth.sso.disabled", u.Email)
		respondError(w, apperr.New(apperr.KindForbidden, "account is disabled"))
		return
	}

	token, err := h.tokens.Issue(u.ID, u.Email, u.Role)
	if err != nil {
		respondError(w, apperr.Wrap(apperr.KindInternal, "issue token", err))
		return
	}
	h.record(ctx, "auth.sso.login", u.Email)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  userPayload{ID: u.ID, Email: u.Email, Role: string(u.Role)},
	})
}

// provision creates a PgFleet user for a first-time SSO identity. The account
// gets a random, unknowable password hash so it can never be used via password
// login — only through the IdP.
func (h *SSOHandler) provision(ctx context.Context, email, groups string) (user.User, error) {
	hash, err := randomPasswordHash()
	if err != nil {
		return user.User{}, err
	}
	u, err := h.store.Create(ctx, user.NewUser{
		Email:        email,
		PasswordHash: hash,
		Role:         h.roleFromGroups(groups),
	})
	if err != nil {
		return user.User{}, err
	}
	h.record(ctx, "auth.sso.provisioned", u.Email)
	return u, nil
}

// roleFromGroups maps the proxy-supplied group membership to a PgFleet role,
// defaulting to the least-privileged viewer.
func (h *SSOHandler) roleFromGroups(groups string) auth.Role {
	for _, g := range strings.FieldsFunc(groups, func(r rune) bool { return r == ',' || r == ' ' }) {
		switch strings.TrimSpace(g) {
		case h.cfg.adminGroup():
			return auth.RoleAdmin
		case h.cfg.operatorGroup():
			return auth.RoleOperator
		}
	}
	return auth.RoleViewer
}

func (h *SSOHandler) record(ctx context.Context, action, target string) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Record(ctx, audit.Entry{Actor: target, Action: action, Target: target})
}

func randomPasswordHash() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", apperr.Wrap(apperr.KindInternal, "sso: generate random secret", err)
	}
	return auth.HashPassword(hex.EncodeToString(b))
}
