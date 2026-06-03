package provision

import (
	"context"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// SetVisibility changes whether an instance's published port is reachable on all
// interfaces (public) or only the secure address (private). A static port
// binding can only change by recreating the container, so a running instance is
// recreated in place on its existing data volume; the change is dynamic from the
// caller's perspective.
func (p *Provisioner) SetVisibility(ctx context.Context, id string, public bool) error {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if inst.Public == public {
		return nil // already in the desired state
	}
	if err := p.repo.SetPublic(ctx, id, public); err != nil {
		return err
	}
	inst.Public = public

	// If it is not running, the new binding takes effect on the next start.
	if inst.Status != instance.StatusRunning || inst.ContainerID == "" {
		return nil
	}

	password, err := p.repo.Password(ctx, id)
	if err != nil {
		return err
	}
	dataVol := inst.DataVolume
	if dataVol == "" {
		dataVol = volumeName("data", id)
	}
	mounts := []docker.Mount{{Volume: dataVol, Path: pgDataPath}}
	if inst.RepoType == instance.RepoLocal {
		mounts = append(mounts, docker.Mount{Volume: volumeName("repo", id), Path: repoPath})
	}

	// Replace the container with one bound to the new address, on the same data.
	old := inst.ContainerID
	timeout := stopTimeoutSeconds
	_ = p.rt.StopContainer(ctx, old, &timeout)
	_ = p.rt.RemoveContainer(ctx, old, true)

	newContainer, err := p.rt.CreateContainer(ctx, p.containerSpec(inst, password, mounts))
	if err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, "visibility change failed: "+err.Error())
		return err
	}
	_ = p.repo.SetRuntime(ctx, id, newContainer, 0)
	if err := p.rt.StartContainer(ctx, newContainer); err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, err.Error())
		return err
	}
	if err := p.waitReady(ctx, newContainer, inst.Superuser); err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, err.Error())
		return err
	}
	// pgbackrest.conf is container-local and is lost on recreate; rewrite it so
	// WAL archiving keeps working.
	if err := p.writeConfig(ctx, newContainer, inst); err != nil {
		return err
	}
	if state, ierr := p.rt.Inspect(ctx, newContainer); ierr == nil {
		if port, perr := assignedPort(state); perr == nil {
			_ = p.repo.SetRuntime(ctx, id, newContainer, port)
		}
	}
	return nil
}
