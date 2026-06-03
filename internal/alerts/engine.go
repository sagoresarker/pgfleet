package alerts

import "fmt"

// Severity escalation lines: past these the same condition is critical rather
// than warning.
const (
	criticalDiskFreePercent  = 5   // disk free below 5% is critical
	criticalReplicationLagS  = 900 // lag over 900s is critical
	criticalConnectionUtilPc = 98  // utilization over 98% is critical
)

// Snapshot is the current per-instance set of values to evaluate. Each metric
// is a pointer so that a MISSING (unknown) value is distinguishable from a real
// zero and never falsely fires.
type Snapshot struct {
	InstanceID                   string
	DiskFreePercent              *float64
	ReplicationLagSeconds        *float64
	BackupAgeSeconds             *float64
	ConnectionUtilizationPercent *float64
}

// Finding is a single evaluated threshold crossing.
type Finding struct {
	Kind      string
	Severity  string
	Message   string
	Value     float64
	Threshold float64
	Firing    bool
}

// Evaluate applies thresholds to a snapshot and returns one Finding per crossed
// threshold. Unknown (nil) metrics never produce a finding.
func Evaluate(snapshot Snapshot, th Thresholds) []Finding {
	var out []Finding

	// disk_full: free disk strictly below the minimum.
	if v := snapshot.DiskFreePercent; v != nil && *v < th.MinDiskFreePercent {
		sev := SeverityWarning
		if *v < criticalDiskFreePercent {
			sev = SeverityCritical
		}
		out = append(out, Finding{
			Kind:      KindDiskFull,
			Severity:  sev,
			Message:   fmt.Sprintf("disk free %.1f%% is below %.1f%%", *v, th.MinDiskFreePercent),
			Value:     *v,
			Threshold: th.MinDiskFreePercent,
			Firing:    true,
		})
	}

	// replication_lag: lag strictly above the maximum.
	if v := snapshot.ReplicationLagSeconds; v != nil && *v > th.MaxReplicationLagSeconds {
		sev := SeverityWarning
		if *v > criticalReplicationLagS {
			sev = SeverityCritical
		}
		out = append(out, Finding{
			Kind:      KindReplicationLag,
			Severity:  sev,
			Message:   fmt.Sprintf("replication lag %.0fs exceeds %.0fs", *v, th.MaxReplicationLagSeconds),
			Value:     *v,
			Threshold: th.MaxReplicationLagSeconds,
			Firing:    true,
		})
	}

	// backup_stale: newest backup older than the maximum age.
	if v := snapshot.BackupAgeSeconds; v != nil && *v > th.MaxBackupAgeSeconds {
		out = append(out, Finding{
			Kind:      KindBackupStale,
			Severity:  SeverityWarning,
			Message:   fmt.Sprintf("last backup %.0fs old exceeds %.0fs", *v, th.MaxBackupAgeSeconds),
			Value:     *v,
			Threshold: th.MaxBackupAgeSeconds,
			Firing:    true,
		})
	}

	// connection_saturation: utilization strictly above the maximum.
	if v := snapshot.ConnectionUtilizationPercent; v != nil && *v > th.MaxConnectionUtilizationPercent {
		sev := SeverityWarning
		if *v > criticalConnectionUtilPc {
			sev = SeverityCritical
		}
		out = append(out, Finding{
			Kind:      KindConnectionSaturation,
			Severity:  sev,
			Message:   fmt.Sprintf("connection utilization %.1f%% exceeds %.1f%%", *v, th.MaxConnectionUtilizationPercent),
			Value:     *v,
			Threshold: th.MaxConnectionUtilizationPercent,
			Firing:    true,
		})
	}

	return out
}
