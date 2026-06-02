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
	_ = p.repo.SetStatus(ctx, id, instance.StatusRestoring, "")

	restoreArgs, err := pgbackrest.Restore(inst.Stanza, confPath, pgbackrest.RestoreOpts{
		Type:         opts.Type,
		Target:       opts.Target,
		TargetAction: targetAction(opts.Type),
		Set:          opts.Set,
		Delta:        true, // restore over the existing data dir
	})
	if err != nil {
		return err
	}

	progress.emit("stopping", "stopping instance for restore")
	if inst.ContainerID != "" {
		timeout := stopTimeoutSeconds
		if err := p.rt.StopContainer(ctx, inst.ContainerID, &timeout); err != nil {
			return err
		}
	}

	progress.emit("restoring", "running pgBackRest restore")
	if err := p.runRestoreContainer(ctx, inst, restoreArgs); err != nil {
		return err
	}

	progress.emit("starting", "starting instance to recover")
	if err := p.rt.StartContainer(ctx, inst.ContainerID); err != nil {
		return err
	}
	// The host port is re-assigned on restart; refresh it before clients reconnect.
	p.refreshPort(ctx, id, inst.ContainerID)
	if err := p.waitReady(ctx, inst.ContainerID, inst.Superuser); err != nil {
		return err
	}
	progress.emit("restored", "restore complete")
	return nil
}

// runRestoreContainer runs a one-shot container that writes the pgbackrest
// config and performs the restore into the shared data volume, then exits.
func (p *Provisioner) runRestoreContainer(ctx context.Context, inst instance.Instance, restoreArgs []string) error {
	conf, err := p.backrestConf(inst)
	if err != nil {
		return err
	}
	script := strings.Join([]string{
		"set -e",
		"mkdir -p /etc/pgbackrest",
		"chown -R postgres:postgres " + repoPath + " " + pgDataPath,
		"umask 0177",
		"cat > " + confPath + " <<'PGBR_EOF'",
		conf,
		"PGBR_EOF",
		"chown postgres:postgres " + confPath,
		shellJoin(asPostgres(restoreArgs)),
	}, "\n")

	mounts := []docker.Mount{{Volume: volumeName("data", inst.ID), Path: pgDataPath}}
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
