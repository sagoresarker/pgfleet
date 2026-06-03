package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/sagoresarker/pgfleet/internal/audit"
)

const (
	// auditDefaultLimit is the page size used when ?limit= is absent, invalid, or
	// non-positive.
	auditDefaultLimit = 100
	// auditMaxLimit caps how many entries a single request may pull, so a client
	// cannot force an unbounded scan of the append-only audit log.
	auditMaxLimit = 500
)

// AuditLister is the read side of the audit log. *audit.Recorder satisfies it
// via its List method; tests inject a fake.
type AuditLister interface {
	List(ctx context.Context, limit int) ([]audit.Entry, error)
}

// AuditHandler serves read access to the append-only audit trail.
//
// RBAC: the audit log records who did what across the whole fleet (including
// privileged data-plane actions), so reading it is gated at the admin level
// (auth.ActionUserManage) in the router — only admins may inspect the trail.
type AuditHandler struct {
	store AuditLister
}

// NewAuditHandler builds an AuditHandler over the given lister.
func NewAuditHandler(store AuditLister) *AuditHandler {
	return &AuditHandler{store: store}
}

// auditEntryPayload is the stable JSON shape for a single audit record. It is
// decoupled from audit.Entry so the wire contract does not shift if internal
// fields are renamed.
type auditEntryPayload struct {
	ID        string         `json:"id"`
	Actor     string         `json:"actor"`
	Action    string         `json:"action"`
	Target    string         `json:"target"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt string         `json:"created_at"`
}

// List returns the most recent audit entries, newest first, as
// {"entries": [...]}. It accepts an optional ?limit= (default 100, clamped to
// 500); an absent/invalid/non-positive value falls back to the default.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := parseAuditLimit(r.URL.Query().Get("limit"))

	entries, err := h.store.List(r.Context(), limit)
	if err != nil {
		respondError(w, err)
		return
	}

	out := make([]auditEntryPayload, 0, len(entries))
	for _, e := range entries {
		out = append(out, auditEntryPayload{
			ID:        e.ID,
			Actor:     e.Actor,
			Action:    e.Action,
			Target:    e.Target,
			Metadata:  e.Metadata,
			CreatedAt: e.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// parseAuditLimit resolves the requested page size to a safe bounded value.
func parseAuditLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return auditDefaultLimit
	}
	if n > auditMaxLimit {
		return auditMaxLimit
	}
	return n
}
