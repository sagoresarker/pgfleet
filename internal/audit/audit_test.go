package audit

import (
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestEntryValidate(t *testing.T) {
	cases := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{"valid", Entry{Actor: "admin@example.com", Action: "instance.create", Target: "inst_1"}, false},
		{"valid without target", Entry{Actor: "system", Action: "backup.run"}, false},
		{"missing actor", Entry{Action: "instance.create"}, true},
		{"missing action", Entry{Actor: "admin@example.com"}, true},
		{"blank actor", Entry{Actor: "   ", Action: "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.entry.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
			if tc.wantErr && apperr.Kind(err) != apperr.KindInvalid {
				t.Errorf("error kind = %v, want KindInvalid", apperr.Kind(err))
			}
		})
	}
}
