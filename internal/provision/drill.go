package provision

import (
	"context"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

// DrillResult is the outcome of a restore drill.
type DrillResult struct {
	OK       bool
	Duration time.Duration
	Detail   string
}

// RestoreDrill verifies that an instance's latest backup is actually
// restorable: it restores into a throwaway volume and validates the resulting
// data directory with pg_controldata. "Backups exist" is not the same as
// "backups restore"; this proves the latter without touching the live instance.
func (p *Provisioner) RestoreDrill(ctx context.Context, id string) (DrillResult, error) {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return DrillResult{}, err
	}

	conf, err := p.backrestConf(inst)
	if err != nil {
		return DrillResult{}, err
	}

	restoreArgs, err := pgbackrest.Restore(inst.Stanza, confPath, pgbackrest.RestoreOpts{})
	if err != nil {
		return DrillResult{}, err
	}

	drillVol := volumeName("drill-data", inst.ID)
	if err := p.rt.CreateVolume(ctx, drillVol, instanceLabels(inst.ID)); err != nil {
		return DrillResult{}, err
	}
	defer func() { _ = p.rt.RemoveVolume(context.Background(), drillVol, true) }()

	script := strings.Join([]string{
		"set -e",
		"mkdir -p /etc/pgbackrest",
		// Only chown the writable drill data dir. The repo is mounted read-only
		// (a drill only READS the live instance's backups), so it must not be
		// chowned, and a writable mount + recursive chown would race the live
		// instance's concurrent backup/archive into that same repo.
		"chown postgres:postgres " + pgDataPath,
		"umask 0177",
		"cat > " + confPath + " <<'PGBR_EOF'",
		conf,
		"PGBR_EOF",
		"chown postgres:postgres " + confPath,
		shellJoin(asPostgres(restoreArgs)),
		// Validate the restored cluster.
		shellJoin(asPostgres([]string{"pg_controldata", pgDataPath})),
	}, "\n")

	mounts := []docker.Mount{{Volume: drillVol, Path: pgDataPath}}
	if inst.RepoType == instance.RepoLocal {
		mounts = append(mounts, docker.Mount{Volume: volumeName("repo", inst.ID), Path: repoPath, ReadOnly: true})
	}
	spec := docker.ContainerSpec{
		Name:  "pgfleet-drill-" + inst.Name + "-" + shortStamp(),
		Image: inst.Image,
		Cmd:   []string{"bash", "-c", script},
		Labels: map[string]string{
			docker.LabelManaged:  "true",
			docker.LabelInstance: inst.ID,
			docker.LabelRole:     "drill",
		},
		Mounts: mounts,
	}
	if p.opts.Network != "" {
		spec.Networks = []string{p.opts.Network}
	}

	start := time.Now()
	cid, err := p.rt.CreateContainer(ctx, spec)
	if err != nil {
		return DrillResult{}, err
	}
	defer func() { _ = p.rt.RemoveContainer(context.Background(), cid, true) }()
	if err := p.rt.StartContainer(ctx, cid); err != nil {
		return DrillResult{}, err
	}

	err = p.waitRestoreExit(ctx, cid)
	dur := time.Since(start)
	if err != nil {
		return DrillResult{OK: false, Duration: dur, Detail: err.Error()}, nil
	}
	return DrillResult{OK: true, Duration: dur, Detail: "restore + pg_controldata succeeded"}, nil
}
