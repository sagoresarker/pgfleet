package instance

import (
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestNewInstanceValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      NewInstance
		wantErr bool
	}{
		{"valid s3", NewInstance{Name: "orders-db", RepoType: RepoS3, Password: "pw-1234567"}, false},
		{"valid local", NewInstance{Name: "a1", RepoType: RepoLocal, Password: "pw-1234567"}, false},
		{"empty name", NewInstance{RepoType: RepoS3, Password: "pw-1234567"}, true},
		{"uppercase name", NewInstance{Name: "OrdersDB", RepoType: RepoS3, Password: "pw-1234567"}, true},
		{"name starts with digit", NewInstance{Name: "1db", RepoType: RepoS3, Password: "pw-1234567"}, true},
		{"name with underscore", NewInstance{Name: "orders_db", RepoType: RepoS3, Password: "pw-1234567"}, true},
		{"invalid repo type", NewInstance{Name: "db", RepoType: "nfs", Password: "pw-1234567"}, true},
		{"short password", NewInstance{Name: "db", RepoType: RepoS3, Password: "short"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr && apperr.Kind(err) != apperr.KindInvalid {
				t.Errorf("kind = %v, want KindInvalid", apperr.Kind(err))
			}
		})
	}
}

func TestStanzaFor(t *testing.T) {
	if got := StanzaFor("orders-db"); got != "orders-db" {
		t.Errorf("StanzaFor = %q, want orders-db", got)
	}
}
