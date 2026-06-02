package auth

import "testing"

func TestAdminCanDoEverything(t *testing.T) {
	for _, a := range AllActions() {
		if !Can(RoleAdmin, a) {
			t.Errorf("admin should be allowed %q", a)
		}
	}
}

func TestViewerCanOnlyRead(t *testing.T) {
	allowed := map[Action]bool{
		ActionInstanceRead: true,
		ActionBackupRead:   true,
		ActionMetricsRead:  true,
	}
	for _, a := range AllActions() {
		got := Can(RoleViewer, a)
		if got != allowed[a] {
			t.Errorf("viewer Can(%q) = %v, want %v", a, got, allowed[a])
		}
	}
}

func TestOperatorCanManageInstancesButNotUsers(t *testing.T) {
	mustAllow := []Action{
		ActionInstanceRead, ActionInstanceWrite, ActionInstanceDelete, ActionInstanceConnect,
		ActionBackupRead, ActionBackupWrite, ActionBackupRestore,
		ActionMetricsRead, ActionAuditRead,
	}
	for _, a := range mustAllow {
		if !Can(RoleOperator, a) {
			t.Errorf("operator should be allowed %q", a)
		}
	}
	if Can(RoleOperator, ActionUserManage) {
		t.Error("operator must NOT manage users")
	}
}

func TestUnknownRoleCanDoNothing(t *testing.T) {
	for _, a := range AllActions() {
		if Can(Role("root"), a) {
			t.Errorf("unknown role should not be allowed %q", a)
		}
	}
}
