package alerts

import (
	"testing"
)

func ptr(f float64) *float64 { return &f }

func findingByKind(fs []Finding, kind string) (Finding, bool) {
	for _, f := range fs {
		if f.Kind == kind {
			return f, true
		}
	}
	return Finding{}, false
}

func TestEvaluateNoSignalsNoFindings(t *testing.T) {
	got := Evaluate(Snapshot{InstanceID: "i1"}, DefaultThresholds())
	if len(got) != 0 {
		t.Fatalf("expected no findings for an all-unknown snapshot, got %+v", got)
	}
}

func TestEvaluateDiskFullWarningAndCritical(t *testing.T) {
	th := DefaultThresholds()

	// 9% free: below the 10% warning threshold, but not below the 5% critical line.
	fs := Evaluate(Snapshot{InstanceID: "i1", DiskFreePercent: ptr(9)}, th)
	f, ok := findingByKind(fs, KindDiskFull)
	if !ok {
		t.Fatalf("expected disk_full finding")
	}
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", f.Severity)
	}
	if f.Value != 9 || f.Threshold != th.MinDiskFreePercent {
		t.Errorf("value/threshold = %v/%v", f.Value, f.Threshold)
	}
	if !f.Firing {
		t.Error("finding should be firing")
	}

	// 4% free: critical.
	fs = Evaluate(Snapshot{InstanceID: "i1", DiskFreePercent: ptr(4)}, th)
	f, _ = findingByKind(fs, KindDiskFull)
	if f.Severity != SeverityCritical {
		t.Errorf("severity = %q, want critical", f.Severity)
	}
}

func TestEvaluateDiskFreeAtBoundaryDoesNotFire(t *testing.T) {
	th := DefaultThresholds()
	// Exactly at the threshold (10%) is acceptable; only strictly below fires.
	fs := Evaluate(Snapshot{InstanceID: "i1", DiskFreePercent: ptr(10)}, th)
	if _, ok := findingByKind(fs, KindDiskFull); ok {
		t.Error("disk free exactly at threshold should not fire")
	}
	// Plenty free.
	fs = Evaluate(Snapshot{InstanceID: "i1", DiskFreePercent: ptr(50)}, th)
	if _, ok := findingByKind(fs, KindDiskFull); ok {
		t.Error("ample free disk should not fire")
	}
}

func TestEvaluateUnknownDiskDoesNotFire(t *testing.T) {
	th := DefaultThresholds()
	fs := Evaluate(Snapshot{InstanceID: "i1", DiskFreePercent: nil}, th)
	if _, ok := findingByKind(fs, KindDiskFull); ok {
		t.Error("unknown disk metric must NOT fire")
	}
}

func TestEvaluateReplicationLag(t *testing.T) {
	th := DefaultThresholds()

	// 301s: over 300 warning, under 900 critical line -> warning.
	fs := Evaluate(Snapshot{InstanceID: "i1", ReplicationLagSeconds: ptr(301)}, th)
	f, ok := findingByKind(fs, KindReplicationLag)
	if !ok {
		t.Fatalf("expected replication_lag finding")
	}
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", f.Severity)
	}

	// 901s: critical.
	fs = Evaluate(Snapshot{InstanceID: "i1", ReplicationLagSeconds: ptr(901)}, th)
	f, _ = findingByKind(fs, KindReplicationLag)
	if f.Severity != SeverityCritical {
		t.Errorf("severity = %q, want critical", f.Severity)
	}

	// Exactly at threshold does not fire.
	fs = Evaluate(Snapshot{InstanceID: "i1", ReplicationLagSeconds: ptr(300)}, th)
	if _, ok := findingByKind(fs, KindReplicationLag); ok {
		t.Error("lag exactly at threshold should not fire")
	}

	// Unknown lag does not fire.
	fs = Evaluate(Snapshot{InstanceID: "i1", ReplicationLagSeconds: nil}, th)
	if _, ok := findingByKind(fs, KindReplicationLag); ok {
		t.Error("unknown lag must not fire")
	}
}

func TestEvaluateBackupStale(t *testing.T) {
	th := DefaultThresholds()
	over := th.MaxBackupAgeSeconds + 1

	fs := Evaluate(Snapshot{InstanceID: "i1", BackupAgeSeconds: ptr(over)}, th)
	f, ok := findingByKind(fs, KindBackupStale)
	if !ok {
		t.Fatalf("expected backup_stale finding")
	}
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", f.Severity)
	}

	// Fresh backup does not fire.
	fs = Evaluate(Snapshot{InstanceID: "i1", BackupAgeSeconds: ptr(60.0)}, th)
	if _, ok := findingByKind(fs, KindBackupStale); ok {
		t.Error("fresh backup should not fire")
	}

	// Unknown backup age does not fire.
	fs = Evaluate(Snapshot{InstanceID: "i1", BackupAgeSeconds: nil}, th)
	if _, ok := findingByKind(fs, KindBackupStale); ok {
		t.Error("unknown backup age must not fire")
	}
}

func TestEvaluateConnectionSaturation(t *testing.T) {
	th := DefaultThresholds()

	// 91%: warning.
	fs := Evaluate(Snapshot{InstanceID: "i1", ConnectionUtilizationPercent: ptr(91)}, th)
	f, ok := findingByKind(fs, KindConnectionSaturation)
	if !ok {
		t.Fatalf("expected connection_saturation finding")
	}
	if f.Severity != SeverityWarning {
		t.Errorf("severity = %q, want warning", f.Severity)
	}

	// 99%: critical.
	fs = Evaluate(Snapshot{InstanceID: "i1", ConnectionUtilizationPercent: ptr(99)}, th)
	f, _ = findingByKind(fs, KindConnectionSaturation)
	if f.Severity != SeverityCritical {
		t.Errorf("severity = %q, want critical", f.Severity)
	}

	// At threshold does not fire.
	fs = Evaluate(Snapshot{InstanceID: "i1", ConnectionUtilizationPercent: ptr(90)}, th)
	if _, ok := findingByKind(fs, KindConnectionSaturation); ok {
		t.Error("utilization at threshold should not fire")
	}

	// Unknown does not fire.
	fs = Evaluate(Snapshot{InstanceID: "i1", ConnectionUtilizationPercent: nil}, th)
	if _, ok := findingByKind(fs, KindConnectionSaturation); ok {
		t.Error("unknown utilization must not fire")
	}
}

func TestEvaluateMultipleSimultaneousFindings(t *testing.T) {
	th := DefaultThresholds()
	snap := Snapshot{
		InstanceID:                   "i1",
		DiskFreePercent:              ptr(2),
		ReplicationLagSeconds:        ptr(1000),
		BackupAgeSeconds:             ptr(th.MaxBackupAgeSeconds + 100),
		ConnectionUtilizationPercent: ptr(95),
	}
	fs := Evaluate(snap, th)
	if len(fs) != 4 {
		t.Fatalf("expected 4 findings, got %d: %+v", len(fs), fs)
	}
	for _, want := range []string{KindDiskFull, KindReplicationLag, KindBackupStale, KindConnectionSaturation} {
		if _, ok := findingByKind(fs, want); !ok {
			t.Errorf("missing finding %q", want)
		}
	}
}

func TestEvaluateFindingHasMessage(t *testing.T) {
	fs := Evaluate(Snapshot{InstanceID: "i1", DiskFreePercent: ptr(1)}, DefaultThresholds())
	f, _ := findingByKind(fs, KindDiskFull)
	if f.Message == "" {
		t.Error("finding should carry a human-readable message")
	}
}
