package auth

// Action is a permission-checked capability in the control plane.
type Action string

const (
	ActionInstanceRead   Action = "instance.read"
	ActionInstanceWrite  Action = "instance.write"  // create / update / start / stop
	ActionInstanceDelete Action = "instance.delete"
	ActionBackupRead     Action = "backup.read"
	ActionBackupWrite    Action = "backup.write" // take / schedule backups
	ActionBackupRestore  Action = "backup.restore"
	ActionMetricsRead    Action = "metrics.read"
	ActionAuditRead      Action = "audit.read"
	ActionUserManage     Action = "user.manage"
)

// AllActions returns every defined action (useful for tests and tooling).
func AllActions() []Action {
	return []Action{
		ActionInstanceRead, ActionInstanceWrite, ActionInstanceDelete,
		ActionBackupRead, ActionBackupWrite, ActionBackupRestore,
		ActionMetricsRead, ActionAuditRead, ActionUserManage,
	}
}

// readActions are available to every authenticated role, including viewers.
var readActions = []Action{ActionInstanceRead, ActionBackupRead, ActionMetricsRead}

// grants maps each role to the set of actions it may perform. Admin is handled
// separately as a superset.
var grants = map[Role]map[Action]bool{
	RoleViewer: toSet(readActions),
	RoleOperator: toSet(append([]Action{
		ActionInstanceWrite, ActionInstanceDelete,
		ActionBackupWrite, ActionBackupRestore,
		ActionAuditRead,
	}, readActions...)),
}

// Can reports whether role is permitted to perform action.
func Can(role Role, action Action) bool {
	if role == RoleAdmin {
		return true
	}
	return grants[role][action]
}

func toSet(actions []Action) map[Action]bool {
	set := make(map[Action]bool, len(actions))
	for _, a := range actions {
		set[a] = true
	}
	return set
}
