package auth

import "testing"

func TestRoleValid(t *testing.T) {
	valid := []Role{RoleAdmin, RoleOperator, RoleViewer}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("Role %q should be valid", r)
		}
	}
	invalid := []Role{"", "root", "Admin", "superuser"}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("Role %q should be invalid", r)
		}
	}
}

func TestParseRole(t *testing.T) {
	r, err := ParseRole("operator")
	if err != nil || r != RoleOperator {
		t.Errorf("ParseRole(operator) = %v, %v", r, err)
	}
	if _, err := ParseRole("nonsense"); err == nil {
		t.Error("ParseRole(nonsense) should error")
	}
}
