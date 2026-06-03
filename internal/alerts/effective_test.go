package alerts

import "testing"

func TestEffectiveThresholdsEmptyRulesReturnsDefaults(t *testing.T) {
	defaults := DefaultThresholds()
	got := EffectiveThresholds(defaults, nil)
	if got != defaults {
		t.Errorf("EffectiveThresholds(defaults, nil) = %+v, want %+v", got, defaults)
	}
}

func TestEffectiveThresholdsEachKindOverridesItsField(t *testing.T) {
	defaults := DefaultThresholds()
	rules := []Rule{
		{Kind: KindDiskFull, Threshold: 25, Enabled: true},
		{Kind: KindReplicationLag, Threshold: 120, Enabled: true},
		{Kind: KindBackupStale, Threshold: 3600, Enabled: true},
		{Kind: KindConnectionSaturation, Threshold: 75, Enabled: true},
	}
	got := EffectiveThresholds(defaults, rules)
	if got.MinDiskFreePercent != 25 {
		t.Errorf("MinDiskFreePercent = %v, want 25", got.MinDiskFreePercent)
	}
	if got.MaxReplicationLagSeconds != 120 {
		t.Errorf("MaxReplicationLagSeconds = %v, want 120", got.MaxReplicationLagSeconds)
	}
	if got.MaxBackupAgeSeconds != 3600 {
		t.Errorf("MaxBackupAgeSeconds = %v, want 3600", got.MaxBackupAgeSeconds)
	}
	if got.MaxConnectionUtilizationPercent != 75 {
		t.Errorf("MaxConnectionUtilizationPercent = %v, want 75", got.MaxConnectionUtilizationPercent)
	}
}

func TestEffectiveThresholdsDisabledRuleIgnored(t *testing.T) {
	defaults := DefaultThresholds()
	rules := []Rule{
		{Kind: KindDiskFull, Threshold: 99, Enabled: false},
	}
	got := EffectiveThresholds(defaults, rules)
	if got.MinDiskFreePercent != defaults.MinDiskFreePercent {
		t.Errorf("disabled rule applied: MinDiskFreePercent = %v, want %v",
			got.MinDiskFreePercent, defaults.MinDiskFreePercent)
	}
}

func TestEffectiveThresholdsUnknownKindIgnored(t *testing.T) {
	defaults := DefaultThresholds()
	rules := []Rule{{Kind: "bogus", Threshold: 1, Enabled: true}}
	if got := EffectiveThresholds(defaults, rules); got != defaults {
		t.Errorf("unknown kind changed thresholds: %+v", got)
	}
}

// Later enabled rules of the same kind win (last write wins).
func TestEffectiveThresholdsLastRuleWins(t *testing.T) {
	defaults := DefaultThresholds()
	rules := []Rule{
		{Kind: KindDiskFull, Threshold: 20, Enabled: true},
		{Kind: KindDiskFull, Threshold: 30, Enabled: true},
	}
	if got := EffectiveThresholds(defaults, rules); got.MinDiskFreePercent != 30 {
		t.Errorf("MinDiskFreePercent = %v, want 30 (last rule wins)", got.MinDiskFreePercent)
	}
}
