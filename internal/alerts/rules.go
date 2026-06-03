package alerts

import (
	"math"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Rule is a user-configurable alert threshold override. It mirrors a row in the
// alert_rules table: InstanceID is nil when the rule applies to ALL instances,
// otherwise it scopes the rule to a single instance.
type Rule struct {
	ID         string    `json:"id"`
	InstanceID *string   `json:"instance_id,omitempty"`
	Kind       string    `json:"kind"`
	Threshold  float64   `json:"threshold"`
	Severity   string    `json:"severity"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// knownKinds is the set of alert kinds a rule may target.
var knownKinds = map[string]bool{
	KindDiskFull:             true,
	KindReplicationLag:       true,
	KindBackupStale:          true,
	KindConnectionSaturation: true,
}

// Validate checks the rule's kind, severity, and threshold. It returns an
// apperr.KindInvalid error so the API boundary maps it to 400.
func (r Rule) Validate() error {
	if !knownKinds[r.Kind] {
		return apperr.New(apperr.KindInvalid, "alert rule: unknown kind "+r.Kind)
	}
	if r.Severity != SeverityWarning && r.Severity != SeverityCritical {
		return apperr.New(apperr.KindInvalid, "alert rule: severity must be warning or critical")
	}
	if math.IsNaN(r.Threshold) || math.IsInf(r.Threshold, 0) {
		return apperr.New(apperr.KindInvalid, "alert rule: threshold must be a finite number")
	}
	return nil
}
