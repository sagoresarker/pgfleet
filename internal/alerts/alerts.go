// Package alerts turns metric and health thresholds into persisted,
// transition-tracked alerts. The evaluation logic is pure (see engine.go); the
// Store reconciles findings against the alerts table so a notifier fires only
// on state transitions.
package alerts

import "time"

// Kind identifies what an alert is about.
const (
	KindDiskFull             = "disk_full"
	KindReplicationLag       = "replication_lag"
	KindBackupStale          = "backup_stale"
	KindConnectionSaturation = "connection_saturation"
)

// Severity classifies how urgent an alert is.
const (
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// State is the lifecycle position of an alert.
const (
	StateFiring   = "firing"
	StateResolved = "resolved"
)

// Alert mirrors a row in the alerts table.
type Alert struct {
	ID         string     `json:"id"`
	InstanceID string     `json:"instance_id"`
	Kind       string     `json:"kind"`
	Severity   string     `json:"severity"`
	State      string     `json:"state"`
	Message    string     `json:"message"`
	Value      *float64   `json:"value,omitempty"`
	Threshold  *float64   `json:"threshold,omitempty"`
	FiredAt    time.Time  `json:"fired_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Thresholds bound the values past which findings fire.
type Thresholds struct {
	// MinDiskFreePercent fires disk_full when free disk drops below this.
	MinDiskFreePercent float64
	// MaxReplicationLagSeconds fires replication_lag when lag exceeds this.
	MaxReplicationLagSeconds float64
	// MaxBackupAgeSeconds fires backup_stale when the newest backup is older.
	MaxBackupAgeSeconds float64
	// MaxConnectionUtilizationPercent fires connection_saturation above this.
	MaxConnectionUtilizationPercent float64
}

// DefaultThresholds returns sensible production defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		MinDiskFreePercent:              10,
		MaxReplicationLagSeconds:        300,
		MaxBackupAgeSeconds:             25 * 60 * 60, // 25h
		MaxConnectionUtilizationPercent: 90,
	}
}
