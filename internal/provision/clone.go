package provision

import (
	"context"
	"strings"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

// quoteIdent / quoteLiteral safely quote a SQL identifier / string literal. The
// statement is passed as a single argv element to psql -c (no shell), so only
// SQL-level quoting is needed.
func quoteIdent(s string) string   { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Clone provisions cloneID as an independent standalone instance whose data is a
// full physical copy of the source, restored from the source's pgBackRest repo
// (latest backup + WAL). The clone gets its own identity: its own superuser
// password, its own stanza, and its own repo for future backups.
func (p *Provisioner) Clone(ctx context.Context, cloneID string, source instance.Instance, progress ProgressFunc) error {
	if err := p.clone(ctx, cloneID, source, progress); err != nil {
		_ = p.repo.SetStatus(ctx, cloneID, instance.StatusError, err.Error())
		return err
	}
	return p.repo.SetStatus(ctx, cloneID, instance.StatusRunning, "")
}

func (p *Provisioner) clone(ctx context.Context, cloneID string, source instance.Instance, progress ProgressFunc) (err error) {
	clone, err := p.repo.Get(ctx, cloneID)
	if err != nil {
		return err
	}
	password, err := p.repo.Password(ctx, cloneID)
	if err != nil {
		return err
	}

	progress.emit("image", "ensuring image "+clone.Image)
	if err := p.rt.EnsureImage(ctx, clone.Image); err != nil {
		return err
	}

	var createdVolumes []string
	var createdContainer string
	defer func() {
		if err == nil {
			return
		}
		bg := context.Background()
		if createdContainer != "" {
			_ = p.rt.RemoveContainer(bg, createdContainer, true)
		}
		for _, v := range createdVolumes {
			_ = p.rt.RemoveVolume(bg, v, true)
		}
	}()

	// Fresh data volume for the clone.
	dataVol := volumeName("data", cloneID)
	progress.emit("volume", "creating clone data volume")
	if err = p.rt.CreateVolume(ctx, dataVol, instanceLabels(cloneID)); err != nil {
		return err
	}
	createdVolumes = append(createdVolumes, dataVol)
	_ = p.repo.SetDataVolume(ctx, cloneID, dataVol)

	// Restore the SOURCE's data into the clone's volume. runRestoreContainer
	// uses the SOURCE's pgbackrest.conf + repo (so it reads the source's
	// backups) and writes into dataVol. Restore to latest, promoting onto a
	// fresh timeline so the clone runs independently.
	progress.emit("restoring", "restoring data from "+source.Name)
	restoreArgs, err := pgbackrest.Restore(source.Stanza, confPath, pgbackrest.RestoreOpts{})
	if err != nil {
		return err
	}
	if err = p.runRestoreContainer(ctx, source, restoreArgs, dataVol); err != nil {
		return err
	}

	// The clone needs its OWN repo volume (local) for future backups.
	mounts := []docker.Mount{{Volume: dataVol, Path: pgDataPath}}
	if clone.RepoType == instance.RepoLocal {
		repoVol := volumeName("repo", cloneID)
		if err = p.rt.CreateVolume(ctx, repoVol, instanceLabels(cloneID)); err != nil {
			return err
		}
		createdVolumes = append(createdVolumes, repoVol)
		mounts = append(mounts, docker.Mount{Volume: repoVol, Path: repoPath})
	}

	progress.emit("container", "starting clone")
	containerID, err := p.rt.CreateContainer(ctx, p.containerSpec(clone, password, mounts))
	if err != nil {
		return err
	}
	createdContainer = containerID
	_ = p.repo.SetRuntime(ctx, cloneID, containerID, 0)
	if err = p.rt.StartContainer(ctx, containerID); err != nil {
		return err
	}

	progress.emit("waiting", "waiting for postgres")
	if err = p.waitReady(ctx, containerID, clone.Superuser); err != nil {
		return err
	}
	if state, ierr := p.rt.Inspect(ctx, containerID); ierr == nil {
		if port, perr := assignedPort(state); perr == nil {
			_ = p.repo.SetRuntime(ctx, cloneID, containerID, port)
		}
	}

	// The restored data carries the SOURCE's superuser password; reset it to the
	// clone's own credential so the stored DSN works.
	progress.emit("credentials", "resetting superuser password")
	if err = p.execOK(ctx, containerID, asPostgres([]string{
		"psql", "-U", clone.Superuser, "-d", "postgres", "-tAc",
		"ALTER USER " + quoteIdent(clone.Superuser) + " WITH PASSWORD " + quoteLiteral(password),
	})); err != nil {
		return err
	}

	// Give the clone its OWN pgBackRest config + stanza so future backups go to
	// its own repo path, not the source's.
	progress.emit("config", "writing clone pgbackrest config")
	if err = p.writeConfig(ctx, containerID, clone); err != nil {
		return err
	}
	progress.emit("stanza", "creating pgBackRest stanza")
	if err = p.execOK(ctx, containerID, asPostgres(pgbackrest.StanzaCreate(clone.Stanza, confPath))); err != nil {
		return err
	}

	// Re-create requested extensions (idempotent; the physical copy already has
	// them, but pg_stat_statements + user extensions are ensured for safety).
	_ = p.execOK(ctx, containerID, createExtension(clone.Superuser, "pg_stat_statements"))
	for _, ext := range clone.Extensions {
		_ = p.execOK(ctx, containerID, createExtension(clone.Superuser, ext))
	}

	progress.emit("ready", "clone is ready")
	return nil
}
