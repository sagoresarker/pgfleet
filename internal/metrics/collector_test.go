package metrics

import "testing"

// TestCheckpointQueryByVersion: PG17 split checkpoint counters out of
// pg_stat_bgwriter into pg_stat_checkpointer (with renamed columns). The
// collector must pick the view by server version so it never issues a statement
// that errors — and spams the server log — on the other major. (Reported: a PG16
// instance logged `relation "pg_stat_checkpointer" does not exist` every poll.)
func TestCheckpointQueryByVersion(t *testing.T) {
	cases := []struct {
		name       string
		versionNum int
		wantView   string
	}{
		{"pg16", 160014, "pg_stat_bgwriter"},
		{"pg15", 150000, "pg_stat_bgwriter"},
		{"pg17 exactly", 170000, "pg_stat_checkpointer"},
		{"pg18", 180001, "pg_stat_checkpointer"},
		{"unknown/zero falls back to bgwriter", 0, "pg_stat_bgwriter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := checkpointQuery(c.versionNum)
			if !contains(q, c.wantView) {
				t.Errorf("checkpointQuery(%d) = %q, want it to use %q", c.versionNum, q, c.wantView)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
