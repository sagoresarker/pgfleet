package provision

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

const restoreTimeout = 5 * time.Minute

// RestoreOptions selects what to restore to.
type RestoreOptions struct {
	// Type: "" (latest), "time", "lsn", "xid", "name".
	Type string
	// Target is the recovery target value (required when Type is set).
	Target string
	// Set restores a specific backup label instead of the latest.
	Set string
}

// Restore performs an in-place restore of an instance: it stops the instance,
// runs `pgbackrest restore` (in a one-shot container sharing the data and repo
// volumes), then starts the instance so Postgres recovers to the target and
// promotes. For PITR pass Type="time" and Target=<timestamp>.
func (p *Provisioner) Restore(ctx context.Context, id string, opts RestoreOptions, progress ProgressFunc) error {
	if err := p.restore(ctx, id, opts, progress); err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, err.Error())
		return err
	}
	return p.repo.SetStatus(ctx, id, instance.StatusRunning, "")
}

func (p *Provisioner) restore(ctx context.Context, id string, opts RestoreOptions, progress ProgressFunc) error {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	// A replica is rebuilt from its primary, not restored. And restoring a
	// clustered PRIMARY in place would promote it onto a new timeline that its
	// replicas can't follow, silently diverging the read pool — refuse it until
	// a replica-reclone restore path exists.
	if inst.Role == instance.RoleReplica {
		return apperr.New(apperr.KindInvalid, "restore: a replica cannot be restored; rebuild it from the primary")
	}
	if inst.Role == instance.RolePrimary && inst.ClusterID != "" {
		return apperr.New(apperr.KindInvalid, "restore: restoring a clustered primary would diverge its replicas; not yet supported")
	}
	oldVol := inst.DataVolume
	if oldVol == "" {
		oldVol = volumeName("data", inst.ID)
	}
	_ = p.repo.SetStatus(ctx, id, instance.StatusRestoring, "")

	// Restore into a FRESH staging volume — the live data volume is never
	// mutated, so a failed restore can never leave the only copy half-written.
	restoreArgs, err := pgbackrest.Restore(inst.Stanza, confPath, pgbackrest.RestoreOpts{
		Type:         opts.Type,
		Target:       opts.Target,
		TargetAction: targetAction(opts.Type),
		Set:          opts.Set,
	})
	if err != nil {
		return err
	}

	newVol := fmt.Sprintf("pgfleet-data-%s-r%s", inst.ID, shortStamp())
	if err := p.rt.CreateVolume(ctx, newVol, instanceLabels(inst.ID)); err != nil {
		return err
	}
	removeNewVol := func() { _ = p.rt.RemoveVolume(context.Background(), newVol, true) }

	// Stop the live instance FIRST (cleanly flushing and archiving its WAL)
	// before restoring. Otherwise the running instance keeps archiving and the
	// recovery's archive-get races it, which can overshoot the recovery target.
	// The original data volume is preserved for rollback.
	progress.emit("stopping", "stopping instance for restore")
	if inst.ContainerID != "" {
		timeout := stopTimeoutSeconds
		_ = p.rt.StopContainer(ctx, inst.ContainerID, &timeout)
	}

	progress.emit("restoring", "restoring into a staging volume")
	if err := p.runRestoreContainer(ctx, inst, restoreArgs, newVol); err != nil {
		removeNewVol()
		p.rollbackRestore(ctx, inst, "", "") // restart the original instance
		return err
	}

	progress.emit("swapping", "promoting the restored volume")
	mounts := []docker.Mount{{Volume: newVol, Path: pgDataPath}}
	if inst.RepoType == instance.RepoLocal {
		mounts = append(mounts, docker.Mount{Volume: volumeName("repo", inst.ID), Path: repoPath})
	}
	password, err := p.repo.Password(ctx, id)
	if err != nil {
		removeNewVol()
		return err
	}
	spec := p.containerSpec(inst, password, mounts)
	// Unique name so the swap container doesn't collide with the still-present
	// original container (kept for rollback until the swap is committed).
	spec.Name = spec.Name + "-r" + shortStamp()
	newContainer, err := p.rt.CreateContainer(ctx, spec)
	if err != nil {
		p.rollbackRestore(ctx, inst, "", newVol)
		return err
	}
	if err := p.rt.StartContainer(ctx, newContainer); err != nil {
		p.rollbackRestore(ctx, inst, newContainer, newVol)
		return err
	}
	if err := p.waitReady(ctx, newContainer, inst.Superuser); err != nil {
		p.rollbackRestore(ctx, inst, newContainer, newVol)
		return err
	}

	// New instance is healthy: commit the swap, then discard the old container
	// and volume.
	if state, ierr := p.rt.Inspect(ctx, newContainer); ierr == nil {
		port, _ := assignedPort(state)
		_ = p.repo.SetRuntime(ctx, id, newContainer, port)
	}
	_ = p.repo.SetDataVolume(ctx, id, newVol)
	if inst.ContainerID != "" {
		_ = p.rt.RemoveContainer(context.Background(), inst.ContainerID, true)
	}
	_ = p.rt.RemoveVolume(context.Background(), oldVol, true)

	progress.emit("restored", "restore complete")
	return nil
}

// rollbackRestore tears down a failed restore's new container/volume and brings
// the original instance back up on its untouched data volume.
func (p *Provisioner) rollbackRestore(ctx context.Context, inst instance.Instance, newContainer, newVol string) {
	bg := context.Background()
	if newContainer != "" {
		_ = p.rt.RemoveContainer(bg, newContainer, true)
	}
	if newVol != "" {
		_ = p.rt.RemoveVolume(bg, newVol, true)
	}
	if inst.ContainerID != "" {
		_ = p.rt.StartContainer(ctx, inst.ContainerID)
		p.refreshPort(ctx, inst.ID, inst.ContainerID)
	}
}

// runRestoreContainer runs a one-shot container that writes the pgbackrest
// config and restores into the given (fresh) data volume, then exits.
func (p *Provisioner) runRestoreContainer(ctx context.Context, inst instance.Instance, restoreArgs []string, dataVolume string) error {
	conf, err := p.backrestConf(inst)
	if err != nil {
		return err
	}
	// Restore into the fresh volume, then run recovery to completion HERE --
	// this container has the pgbackrest.conf needed for archive-get during WAL
	// replay -- producing a promoted, ready-to-run cluster. The instance
	// container can then simply start it without needing recovery config.
	script := strings.Join([]string{
		"set -e",
		"mkdir -p /etc/pgbackrest",
		"chown postgres:postgres " + repoPath + " " + pgDataPath,
		"umask 0177",
		"cat > " + confPath + " <<'PGBR_EOF'",
		conf,
		"PGBR_EOF",
		"chown postgres:postgres " + confPath,
		shellJoin(asPostgres(restoreArgs)),
		// Drive recovery to the target and promote, then shut down cleanly.
		"gosu postgres pg_ctl -D " + pgDataPath + " -w -t 600 start",
		"gosu postgres pg_ctl -D " + pgDataPath + " -m fast -w stop",
	}, "\n")

	mounts := []docker.Mount{{Volume: dataVolume, Path: pgDataPath}}
	if inst.RepoType == instance.RepoLocal {
		mounts = append(mounts, docker.Mount{Volume: volumeName("repo", inst.ID), Path: repoPath})
	}
	spec := docker.ContainerSpec{
		Name:  "pgfleet-restore-" + inst.Name + "-" + shortStamp(),
		Image: inst.Image,
		Cmd:   []string{"bash", "-c", script},
		Labels: map[string]string{
			docker.LabelManaged:  "true",
			docker.LabelInstance: inst.ID,
			docker.LabelRole:     "restore",
		},
		Mounts: mounts,
	}
	if p.opts.Network != "" {
		spec.Networks = []string{p.opts.Network}
	}

	cid, err := p.rt.CreateContainer(ctx, spec)
	if err != nil {
		return err
	}
	defer func() { _ = p.rt.RemoveContainer(context.Background(), cid, true) }()

	if err := p.rt.StartContainer(ctx, cid); err != nil {
		return err
	}
	return p.waitRestoreExit(ctx, cid)
}

// waitRestoreExit blocks until the one-shot restore container exits, returning
// an error (with logs) if it exits non-zero.
func (p *Provisioner) waitRestoreExit(ctx context.Context, cid string) error {
	deadline := time.Now().Add(restoreTimeout)
	for {
		state, err := p.rt.Inspect(ctx, cid)
		if err != nil {
			return err
		}
		if !state.Running && state.Status == "exited" {
			if state.ExitCode != 0 {
				logs := p.containerLogs(ctx, cid)
				return apperr.New(apperr.KindInternal,
					fmt.Sprintf("provision: restore failed (exit %d): %s", state.ExitCode, logs))
			}
			return nil
		}
		if time.Now().After(deadline) {
			return apperr.New(apperr.KindInternal, "provision: restore timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (p *Provisioner) containerLogs(ctx context.Context, cid string) string {
	rc, err := p.rt.Logs(ctx, cid, false)
	if err != nil {
		return ""
	}
	defer rc.Close()
	// Read the full logs (bounded) so diagnostics aren't truncated, and redact
	// any S3 secret echoed by the config heredoc.
	data, _ := io.ReadAll(io.LimitReader(rc, 64*1024))
	return redactSecrets(strings.TrimSpace(string(data)))
}

// targetAction returns the recovery target action. For a targeted PITR we
// promote so the cluster opens read-write at the target.
func targetAction(restoreType string) string {
	if restoreType == "" {
		return ""
	}
	return "promote"
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

var stampCounter atomic.Int64

// shortStamp returns a unique suffix for throwaway container names. It combines
// a nanosecond timestamp with an atomic counter so concurrent restores/drills
// never collide.
func shortStamp() string {
	return fmt.Sprintf("%x-%d", time.Now().UnixNano()&0xffffff, stampCounter.Add(1))
}
