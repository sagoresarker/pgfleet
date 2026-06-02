package user

import (
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
)

func TestNewUserValidate(t *testing.T) {
	cases := []struct {
		name    string
		in      NewUser
		wantErr bool
	}{
		{"valid", NewUser{Email: "a@b.com", PasswordHash: "$argon2id$...", Role: auth.RoleAdmin}, false},
		{"missing email", NewUser{PasswordHash: "h", Role: auth.RoleViewer}, true},
		{"blank email", NewUser{Email: "  ", PasswordHash: "h", Role: auth.RoleViewer}, true},
		{"missing hash", NewUser{Email: "a@b.com", Role: auth.RoleViewer}, true},
		{"invalid role", NewUser{Email: "a@b.com", PasswordHash: "h", Role: "root"}, true},
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

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Foo@BAR.com "); got != "foo@bar.com" {
		t.Errorf("NormalizeEmail = %q, want foo@bar.com", got)
	}
}
