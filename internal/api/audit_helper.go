package api

import (
	"net/http"

	"github.com/sagoresarker/pgfleet/internal/audit"
	"github.com/sagoresarker/pgfleet/internal/auth"
)

// recordAudit writes an audit entry for the authenticated actor of r. The
// recorder may be nil (recording is skipped). Failures are intentionally
// swallowed so auditing never blocks the primary request.
func recordAudit(rec AuditRecorder, r *http.Request, action, target string) {
	if rec == nil {
		return
	}
	actor := "system"
	if c, ok := auth.ClaimsFromContext(r.Context()); ok && c.Email != "" {
		actor = c.Email
	}
	_ = rec.Record(r.Context(), audit.Entry{Actor: actor, Action: action, Target: target})
}
