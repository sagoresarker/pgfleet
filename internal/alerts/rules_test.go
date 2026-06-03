package alerts

import (
	"math"
	"testing"
)

func TestRuleValidateAcceptsKnownKinds(t *testing.T) {
	for _, kind := range []string{
		KindDiskFull, KindReplicationLag, KindBackupStale, KindConnectionSaturation,
	} {
		r := Rule{Kind: kind, Severity: SeverityWarning, Threshold: 10}
		if err := r.Validate(); err != nil {
			t.Errorf("kind %q: unexpected error %v", kind, err)
		}
	}
}

func TestRuleValidateRejectsUnknownKind(t *testing.T) {
	r := Rule{Kind: "cpu_on_fire", Severity: SeverityWarning, Threshold: 10}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}
}

func TestRuleValidateRejectsBadSeverity(t *testing.T) {
	for _, sev := range []string{"", "info", "fatal", "Warning"} {
		r := Rule{Kind: KindDiskFull, Severity: sev, Threshold: 10}
		if err := r.Validate(); err == nil {
			t.Errorf("severity %q: expected error, got nil", sev)
		}
	}
	for _, sev := range []string{SeverityWarning, SeverityCritical} {
		r := Rule{Kind: KindDiskFull, Severity: sev, Threshold: 10}
		if err := r.Validate(); err != nil {
			t.Errorf("severity %q: unexpected error %v", sev, err)
		}
	}
}

func TestRuleValidateRejectsNonFiniteThreshold(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		r := Rule{Kind: KindDiskFull, Severity: SeverityWarning, Threshold: v}
		if err := r.Validate(); err == nil {
			t.Errorf("threshold %v: expected error, got nil", v)
		}
	}
}
