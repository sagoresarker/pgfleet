package pgbackrest

import "testing"

// TestParseInfoMissingTimestampIsZeroNot1970 — a backup whose JSON omits the
// timestamp object decodes start/stop as 0. Mapping that through time.Unix(0)
// yields 1970, which corrupts catalog ordering (List orders by stopped_at) and
// retention/PITR displays. A missing timestamp must be a zero time.Time.
func TestParseInfoMissingTimestampIsZeroNot1970(t *testing.T) {
	js := `[{"name":"db","status":{"code":0,"message":"ok"},"backup":[
	   {"label":"L","type":"full","info":{"size":1},"archive":{"start":"a","stop":"b"}}]}]`
	stanzas, err := ParseInfo([]byte(js))
	if err != nil {
		t.Fatalf("ParseInfo: %v", err)
	}
	b := stanzas[0].Backups[0]
	if !b.StartTime.IsZero() {
		t.Errorf("missing start timestamp should be zero time, got %v", b.StartTime)
	}
	if !b.StopTime.IsZero() {
		t.Errorf("missing stop timestamp should be zero time, got %v", b.StopTime)
	}
}

// TestParseInfoErrorFlagAndMultipleStanzas — the per-backup error flag is
// preserved and multiple stanzas in one payload are all parsed.
func TestParseInfoErrorFlagAndMultipleStanzas(t *testing.T) {
	js := `[
	  {"name":"a","status":{"code":0},"backup":[{"label":"L1","type":"full","timestamp":{"start":1717416000,"stop":1717416060},"error":true}]},
	  {"name":"b","status":{"code":0},"backup":[]}
	]`
	stanzas, err := ParseInfo([]byte(js))
	if err != nil {
		t.Fatalf("ParseInfo: %v", err)
	}
	if len(stanzas) != 2 {
		t.Fatalf("got %d stanzas, want 2", len(stanzas))
	}
	if !stanzas[0].Backups[0].Error {
		t.Error("error:true flag should be preserved")
	}
	if stanzas[0].Backups[0].StartTime.Unix() != 1717416000 {
		t.Errorf("present timestamp should still parse, got %v", stanzas[0].Backups[0].StartTime)
	}
}
