package pgbackrest

import (
	"fmt"
	"sort"
)

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

// BackupOpts parameterizes a backup beyond its type.
type BackupOpts struct {
	// Annotations are arbitrary key/value pairs stored on the backup set and
	// surfaced back by `info --output=json`. They are emitted as
	// --annotation=key=value, sorted by key for a deterministic command. Use the
	// "name" key to give a backup a human-readable label in the UI.
	Annotations map[string]string
	// BackupStandby, when true, takes the backup from a standby (replica) so the
	// primary is not loaded. It only has an effect when the stanza has a reachable
	// standby (pgN-* host configured); otherwise pgBackRest falls back to the
	// primary.
	BackupStandby bool
}

// Backup builds a backup command of the given type (full|incr|diff) with the
// given options.
func Backup(stanza, confPath, backupType string, o BackupOpts) ([]string, error) {
	if !backupTypes[backupType] {
		return nil, fmt.Errorf("pgbackrest: invalid backup type %q", backupType)
	}
	args := append(base(stanza, confPath), "--type="+backupType)
	if o.BackupStandby {
		args = append(args, "--backup-standby")
	}
	// Sort annotation keys for a deterministic argv. Empty keys are skipped
	// (pgBackRest rejects an --annotation with no key).
	keys := make([]string, 0, len(o.Annotations))
	for k := range o.Annotations {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--annotation="+k+"="+o.Annotations[k])
	}
	return append(args, "backup"), nil
}

// Info builds the machine-readable catalog query.
func Info(stanza, confPath string) []string {
	return append(base(stanza, confPath), "--output=json", "info")
}

// Verify builds a `verify` command, which checks the integrity of the
// repository (backup files and WAL) for the stanza without restoring.
func Verify(stanza, confPath string) []string {
	return append(base(stanza, confPath), "verify")
}

// Expire enforces the configured retention policy.
func Expire(stanza, confPath string) []string {
	return append(base(stanza, confPath), "expire")
}

// ExpireSet deletes a single backup set identified by its label via
// `expire --set=<label>`, leaving the rest of the retention untouched.
func ExpireSet(stanza, confPath, set string) ([]string, error) {
	if set == "" {
		return nil, fmt.Errorf("pgbackrest: expire --set requires a backup label")
	}
	return append(base(stanza, confPath), "--set="+set, "expire"), nil
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
	// Repo selects which repository to restore from (1 or 2); 0 lets pgbackrest
	// pick (repo1). Use 2 to recover from the second repo when repo1 is lost.
	Repo int
}

// Restore builds a restore command.
func Restore(stanza, confPath string, o RestoreOpts) ([]string, error) {
	if o.Type != "" && o.Target == "" {
		return nil, fmt.Errorf("pgbackrest: restore --type=%s requires a target", o.Type)
	}
	args := base(stanza, confPath)
	if o.Repo > 0 {
		args = append(args, fmt.Sprintf("--repo=%d", o.Repo))
	}
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
