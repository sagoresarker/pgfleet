package provision

import (
	"context"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// SetVisibility changes whether an instance's published port is reachable on all
// interfaces (public) or only the secure address (private). A static port
// binding can only change by recreating the container, so a running instance is
// recreated in place on its existing data volume; the change is dynamic from the
// caller's perspective.
//
// The flip is guarded so it is safe in production:
//   - only a standalone single-node instance may flip (recreating a replica or a
//     clustered member from a standalone spec would corrupt it) — VIS-2;
//   - it is refused unless the instance is in a stable state (running/stopped),
//     never mid-provision/restore — REG-2;
//   - flips for one instance are serialized so concurrent calls cannot both tear
//     down and recreate the container — REG-2;
//   - if creating the replacement container fails after the old one was removed,
//     the instance is restored to a running container rather than left wedged
//     with no container — VIS-1/X-1.
func (p *Provisioner) SetVisibility(ctx context.Context, id string, public bool) error {
	// Serialize flips per instance so two concurrent calls cannot race on the
	// remove/create sequence.
	mu := p.instanceVisMutex(id)
	mu.Lock()
	defer mu.Unlock()

	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	// VIS-2: only a standalone, single-node instance may flip visibility. A
	// replica is rebuilt from its primary, and a clustered member is bound to its
	// cluster topology; recreating either from this single-node spec would corrupt
	// it.
	if inst.Role == instance.RoleReplica || inst.ClusterID != "" {
		return apperr.New(apperr.KindInvalid,
			"visibility: only a standalone instance can change visibility")
	}

	// REG-2: refuse the flip unless the instance is in a stable state. A flip
	// while provisioning/restoring/destroying/errored would race the owning
	// operation or recreate a half-built container.
	switch inst.Status {
	case instance.StatusRunning, instance.StatusStopped:
		// ok — stable
	default:
		return apperr.New(apperr.KindInvalid,
			"visibility: instance is not in a stable state (status "+string(inst.Status)+")")
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
		// VIS-1/X-1: the old container is already gone. Leaving the instance with
		// no container and StatusError would wedge it forever (the reconciler
		// skips StatusError and never recreates a container). Instead, recover by
		// recreating the container with the ORIGINAL binding so the instance keeps
		// running; the visibility flag is rolled back to match. Only if recovery
		// also fails (Docker genuinely unavailable) do we surface an error — and
		// even then we leave it StatusStopped (a healable state), never wedged.
		return p.recoverFailedFlip(ctx, id, inst, password, !public, mounts, err)
	}
	_ = p.repo.SetRuntime(ctx, id, newContainer, 0)
	if err := p.rt.StartContainer(ctx, newContainer); err != nil {
		return p.recoverFailedFlip(ctx, id, inst, password, !public, mounts, err)
	}
	if err := p.waitReady(ctx, newContainer, inst.Superuser); err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, err.Error())
		return err
	}
	// pgbackrest.conf is container-local and is lost on recreate; rewrite it so
	// WAL archiving keeps working. VIS-3: if this fails the instance is running
	// with broken archiving — surface an error status rather than silently
	// returning to "running".
	if err := p.writeConfig(ctx, newContainer, inst); err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError,
			"visibility: archiving config could not be written: "+err.Error())
		return err
	}
	if state, ierr := p.rt.Inspect(ctx, newContainer); ierr == nil {
		if port, perr := assignedPort(state); perr == nil {
			_ = p.repo.SetRuntime(ctx, id, newContainer, port)
		}
	}
	_ = p.repo.SetStatus(ctx, id, instance.StatusRunning, "")
	return nil
}

// recoverFailedFlip is invoked when the replacement container could not be
// created/started after the original was removed. It rolls the visibility flag
// back and tries to bring the instance back up on its original binding so it is
// never left container-less. The original cause is always returned.
func (p *Provisioner) recoverFailedFlip(ctx context.Context, id string, inst instance.Instance, password string, originalPublic bool, mounts []docker.Mount, cause error) error {
	// Roll the persisted visibility back to what it was before the flip.
	_ = p.repo.SetPublic(ctx, id, originalPublic)
	inst.Public = originalPublic

	recovered, rerr := p.rt.CreateContainer(ctx, p.containerSpec(inst, password, mounts))
	if rerr != nil {
		// Could not recreate at all: leave it stopped (a healable state the
		// reconciler/Start can act on), not wedged in error.
		_ = p.repo.SetStatus(ctx, id, instance.StatusStopped,
			"visibility change failed and container could not be recreated: "+cause.Error())
		return cause
	}
	_ = p.repo.SetRuntime(ctx, id, recovered, 0)
	if serr := p.rt.StartContainer(ctx, recovered); serr != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusStopped,
			"visibility change failed; container recreated but not started: "+cause.Error())
		return cause
	}
	_ = p.writeConfig(ctx, recovered, inst)
	if state, ierr := p.rt.Inspect(ctx, recovered); ierr == nil {
		if port, perr := assignedPort(state); perr == nil {
			_ = p.repo.SetRuntime(ctx, id, recovered, port)
		}
	}
	// The instance is back up on its original binding; report the failure but the
	// instance is running and not wedged.
	_ = p.repo.SetStatus(ctx, id, instance.StatusRunning,
		"visibility change failed; reverted to previous binding: "+cause.Error())
	return cause
}
