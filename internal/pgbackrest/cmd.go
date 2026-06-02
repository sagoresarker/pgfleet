package pgbackrest

import "fmt"

// base returns the leading args common to every pgbackrest invocation.
func base(stanza, confPath string) []string {
	return []string{"pgbackrest", "--config=" + confPath, "--stanza=" + stanza}
}

// StanzaCreate initializes the repository for a stanza.
func StanzaCreate(stanza, confPath string) []string {
	return append(base(stanza, confPath), "stanza-create")
}

// Check validates that WAL archiving round-trips to the repository.
func Check(stanza, confPath string) []string {
	return append(base(stanza, confPath), "check")
}

var backupTypes = map[string]bool{"full": true, "incr": true, "diff": true}

// Backup builds a backup command of the given type (full|incr|diff).
func Backup(stanza, confPath, backupType string) ([]string, error) {
	if !backupTypes[backupType] {
		return nil, fmt.Errorf("pgbackrest: invalid backup type %q", backupType)
	}
	return append(base(stanza, confPath), "--type="+backupType, "backup"), nil
}

// Info builds the machine-readable catalog query.
func Info(stanza, confPath string) []string {
	return append(base(stanza, confPath), "--output=json", "info")
}

// Expire enforces the configured retention policy.
func Expire(stanza, confPath string) []string {
	return append(base(stanza, confPath), "expire")
}

// RestoreOpts parameterizes a restore.
type RestoreOpts struct {
	// Type selects the recovery target type: "" (latest), "time", "lsn",
	// "xid", "name".
	Type string
	// Target is the recovery target value (required when Type is set).
	Target string
	// TargetAction is what to do on reaching the target, e.g. "promote".
	TargetAction string
	// Set restores a specific backup label instead of the latest.
	Set string
	// Delta restores only changed files into an existing data dir.
	Delta bool
}

// Restore builds a restore command.
func Restore(stanza, confPath string, o RestoreOpts) ([]string, error) {
	if o.Type != "" && o.Target == "" {
		return nil, fmt.Errorf("pgbackrest: restore --type=%s requires a target", o.Type)
	}
	args := base(stanza, confPath)
	if o.Type != "" {
		args = append(args, "--type="+o.Type, "--target="+o.Target)
		if o.TargetAction != "" {
			args = append(args, "--target-action="+o.TargetAction)
		}
	}
	if o.Set != "" {
		args = append(args, "--set="+o.Set)
	}
	if o.Delta {
		args = append(args, "--delta")
	}
	return append(args, "restore"), nil
}
