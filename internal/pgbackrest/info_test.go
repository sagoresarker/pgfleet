package pgbackrest

import "testing"

// sampleInfoJSON is a representative `pgbackrest info --output=json` payload
// with a full and a dependent incremental backup.
const sampleInfoJSON = `[
  {
    "name": "orders-db",
    "status": {"code": 0, "message": "ok"},
    "db": [{"id": 1, "version": "16", "system-id": 7000000000000000000}],
    "backup": [
      {
        "label": "20260603-120000F",
        "type": "full",
        "timestamp": {"start": 1717416000, "stop": 1717416060},
        "info": {"size": 1048576, "delta": 1048576, "repository": {"size": 524288, "delta": 524288}},
        "archive": {"start": "000000010000000000000002", "stop": "000000010000000000000003"},
        "reference": null,
        "error": false,
        "database": {"id": 1, "repo-key": 1}
      },
      {
        "label": "20260603-130000F_20260603-130500I",
        "type": "incr",
        "timestamp": {"start": 1717419900, "stop": 1717419930},
        "info": {"size": 2097152, "delta": 262144, "repository": {"size": 1048576, "delta": 131072}},
        "archive": {"start": "000000010000000000000005", "stop": "000000010000000000000006"},
        "reference": ["20260603-120000F"],
        "error": false,
        "database": {"id": 1, "repo-key": 1}
      }
    ]
  }
]`

func TestParseInfoBasic(t *testing.T) {
	stanzas, err := ParseInfo([]byte(sampleInfoJSON))
	if err != nil {
		t.Fatalf("ParseInfo: %v", err)
	}
	if len(stanzas) != 1 {
		t.Fatalf("len(stanzas) = %d, want 1", len(stanzas))
	}
	s := stanzas[0]
	if s.Name != "orders-db" || s.StatusCode != 0 {
		t.Errorf("stanza = %+v", s)
	}
	if len(s.Backups) != 2 {
		t.Fatalf("len(backups) = %d, want 2", len(s.Backups))
	}
}

func TestParseInfoFullBackupFields(t *testing.T) {
	stanzas, _ := ParseInfo([]byte(sampleInfoJSON))
	b := stanzas[0].Backups[0]

	if b.Label != "20260603-120000F" || b.Type != "full" {
		t.Errorf("label/type = %q/%q", b.Label, b.Type)
	}
	// Epoch seconds -> UTC time.
	if b.StartTime.Unix() != 1717416000 || b.StopTime.Unix() != 1717416060 {
		t.Errorf("timestamps = %v / %v", b.StartTime, b.StopTime)
	}
	if b.Size != 1048576 || b.RepoSize != 524288 {
		t.Errorf("sizes: logical=%d repo=%d", b.Size, b.RepoSize)
	}
	if b.WALStart != "000000010000000000000002" || b.WALStop != "000000010000000000000003" {
		t.Errorf("WAL range = %q..%q", b.WALStart, b.WALStop)
	}
	if len(b.References) != 0 {
		t.Errorf("full backup should have no references, got %v", b.References)
	}
}

func TestParseInfoIncrementalReferences(t *testing.T) {
	stanzas, _ := ParseInfo([]byte(sampleInfoJSON))
	b := stanzas[0].Backups[1]

	if b.Type != "incr" {
		t.Errorf("type = %q, want incr", b.Type)
	}
	if len(b.References) != 1 || b.References[0] != "20260603-120000F" {
		t.Errorf("references = %v", b.References)
	}
	if b.RepoDelta != 131072 {
		t.Errorf("repo delta = %d, want 131072", b.RepoDelta)
	}
}

func TestParseInfoEmpty(t *testing.T) {
	stanzas, err := ParseInfo([]byte(`[]`))
	if err != nil {
		t.Fatalf("ParseInfo([]): %v", err)
	}
	if len(stanzas) != 0 {
		t.Errorf("want 0 stanzas, got %d", len(stanzas))
	}
}

func TestParseInfoStanzaWithoutBackups(t *testing.T) {
	js := `[{"name":"new-db","status":{"code":2,"message":"missing stanza path"},"backup":[],"db":[]}]`
	stanzas, err := ParseInfo([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if stanzas[0].StatusCode != 2 || stanzas[0].StatusMessage != "missing stanza path" {
		t.Errorf("status = %d/%q", stanzas[0].StatusCode, stanzas[0].StatusMessage)
	}
	if len(stanzas[0].Backups) != 0 {
		t.Errorf("want no backups, got %d", len(stanzas[0].Backups))
	}
}

func TestParseInfoRejectsGarbage(t *testing.T) {
	if _, err := ParseInfo([]byte("not json")); err == nil {
		t.Error("ParseInfo of garbage should error")
	}
}

func TestParseInfoHealthy(t *testing.T) {
	stanzas, _ := ParseInfo([]byte(sampleInfoJSON))
	if !stanzas[0].Healthy() {
		t.Error("status code 0 should be healthy")
	}
	bad := []byte(`[{"name":"x","status":{"code":3,"message":"err"},"backup":[]}]`)
	s, _ := ParseInfo(bad)
	if s[0].Healthy() {
		t.Error("non-zero status code should be unhealthy")
	}
}
