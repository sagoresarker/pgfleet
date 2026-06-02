package pgbackrest

import (
	"reflect"
	"testing"
)

const conf = "/etc/pgbackrest/pgbackrest.conf"

func TestStanzaCreateCmd(t *testing.T) {
	got := StanzaCreate("orders-db", conf)
	want := []string{"pgbackrest", "--config=" + conf, "--stanza=orders-db", "stanza-create"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StanzaCreate = %v, want %v", got, want)
	}
}

func TestCheckCmd(t *testing.T) {
	got := Check("orders-db", conf)
	want := []string{"pgbackrest", "--config=" + conf, "--stanza=orders-db", "check"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Check = %v, want %v", got, want)
	}
}

func TestBackupCmd(t *testing.T) {
	cases := map[string][]string{
		"full": {"pgbackrest", "--config=" + conf, "--stanza=db", "--type=full", "backup"},
		"incr": {"pgbackrest", "--config=" + conf, "--stanza=db", "--type=incr", "backup"},
		"diff": {"pgbackrest", "--config=" + conf, "--stanza=db", "--type=diff", "backup"},
	}
	for typ, want := range cases {
		got, err := Backup("db", conf, typ)
		if err != nil {
			t.Fatalf("Backup(%s): %v", typ, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Backup(%s) = %v, want %v", typ, got, want)
		}
	}
}

func TestBackupRejectsBadType(t *testing.T) {
	if _, err := Backup("db", conf, "bogus"); err == nil {
		t.Error("Backup with bad type should error")
	}
}

func TestInfoCmd(t *testing.T) {
	got := Info("db", conf)
	want := []string{"pgbackrest", "--config=" + conf, "--stanza=db", "--output=json", "info"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Info = %v, want %v", got, want)
	}
}

func TestExpireCmd(t *testing.T) {
	got := Expire("db", conf)
	want := []string{"pgbackrest", "--config=" + conf, "--stanza=db", "expire"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expire = %v, want %v", got, want)
	}
}

func TestRestoreLatest(t *testing.T) {
	got, err := Restore("db", conf, RestoreOpts{Delta: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pgbackrest", "--config=" + conf, "--stanza=db", "--delta", "restore"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Restore latest = %v, want %v", got, want)
	}
}

func TestRestorePITRTime(t *testing.T) {
	got, err := Restore("db", conf, RestoreOpts{
		Type:         "time",
		Target:       "2026-06-03 12:00:00+00",
		TargetAction: "promote",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pgbackrest", "--config=" + conf, "--stanza=db",
		"--type=time", "--target=2026-06-03 12:00:00+00", "--target-action=promote",
		"restore",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Restore PITR = %v, want %v", got, want)
	}
}

func TestRestoreBySet(t *testing.T) {
	got, err := Restore("db", conf, RestoreOpts{Set: "20260603-120000F"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pgbackrest", "--config=" + conf, "--stanza=db", "--set=20260603-120000F", "restore"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Restore by set = %v, want %v", got, want)
	}
}

func TestRestoreRejectsTimeWithoutTarget(t *testing.T) {
	if _, err := Restore("db", conf, RestoreOpts{Type: "time"}); err == nil {
		t.Error("--type=time without target should error")
	}
}
