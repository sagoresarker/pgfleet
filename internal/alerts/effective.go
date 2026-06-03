package alerts

// EffectiveThresholds applies each enabled rule's threshold to the matching
// field of the base thresholds, overriding the default. It is a pure function:
// disabled rules and rules of unknown kinds are ignored, and when several
// enabled rules target the same kind the last one wins. An empty rule set
// returns the defaults unchanged.
func EffectiveThresholds(defaults Thresholds, rules []Rule) Thresholds {
	out := defaults
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Kind {
		case KindDiskFull:
			out.MinDiskFreePercent = r.Threshold
		case KindReplicationLag:
			out.MaxReplicationLagSeconds = r.Threshold
		case KindBackupStale:
			out.MaxBackupAgeSeconds = r.Threshold
		case KindConnectionSaturation:
			out.MaxConnectionUtilizationPercent = r.Threshold
		}
	}
	return out
}
