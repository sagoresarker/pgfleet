package provision

import (
	"context"
	"fmt"
	"net/url"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

const stopTimeoutSeconds = 30

// Start starts a stopped instance's container.
func (p *Provisioner) Start(ctx context.Context, id string) error {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := p.rt.StartContainer(ctx, inst.ContainerID); err != nil {
		return err
	}
	// An ephemeral host port is re-assigned on each start; refresh it.
	p.refreshPort(ctx, id, inst.ContainerID)
	return p.repo.SetStatus(ctx, id, instance.StatusRunning, "")
}

// refreshPort re-reads the container's assigned host port and persists it.
func (p *Provisioner) refreshPort(ctx context.Context, id, containerID string) {
	state, err := p.rt.Inspect(ctx, containerID)
	if err != nil {
		return
	}
	if port, err := assignedPort(state); err == nil {
		_ = p.repo.SetRuntime(ctx, id, containerID, port)
	}
}

// Stop stops a running instance's container (clean fast shutdown).
func (p *Provisioner) Stop(ctx context.Context, id string) error {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	timeout := stopTimeoutSeconds
	if err := p.rt.StopContainer(ctx, inst.ContainerID, &timeout); err != nil {
		return err
	}
	return p.repo.SetStatus(ctx, id, instance.StatusStopped, "")
}

// Restart stops then starts an instance.
func (p *Provisioner) Restart(ctx context.Context, id string) error {
	if err := p.Stop(ctx, id); err != nil {
		return err
	}
	return p.Start(ctx, id)
}

// Destroy removes an instance's container and data volume, and its backup
// repository volume unless retainBackups is set, then deletes its row. Already
// removed resources (NotFound) are tolerated so a partially-provisioned or
// previously-failed instance can always be destroyed. On a real removal
// failure the instance is marked errored (not left wedged in "destroying").
func (p *Provisioner) Destroy(ctx context.Context, id string, retainBackups bool) error {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	_ = p.repo.SetStatus(ctx, id, instance.StatusDestroying, "")

	fail := func(err error) error {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, "destroy failed: "+err.Error())
		return err
	}

	if inst.ContainerID != "" {
		if err := p.rt.RemoveContainer(ctx, inst.ContainerID, true); err != nil && apperr.Kind(err) != apperr.KindNotFound {
			return fail(err)
		}
	}
	dataVol := inst.DataVolume
	if dataVol == "" {
		dataVol = volumeName("data", id)
	}
	if err := p.rt.RemoveVolume(ctx, dataVol, true); err != nil && apperr.Kind(err) != apperr.KindNotFound {
		return fail(err)
	}
	if !retainBackups && inst.RepoType == instance.RepoLocal {
		if err := p.rt.RemoveVolume(ctx, volumeName("repo", id), true); err != nil && apperr.Kind(err) != apperr.KindNotFound {
			return fail(err)
		}
	}
	return p.repo.Delete(ctx, id)
}

// MarkError flags an instance as errored with the given reason. It is used to
// surface an async preflight failure (e.g. the source backup for a clone could
// not be produced) on the target instance, so it does not linger in
// "provisioning". A missing instance is tolerated.
func (p *Provisioner) MarkError(ctx context.Context, id, reason string) error {
	return p.repo.SetStatus(ctx, id, instance.StatusError, reason)
}

// DSN builds a connection string for clients, using the configured instance
// host and the instance's assigned port.
func (p *Provisioner) DSN(ctx context.Context, id string) (string, error) {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}
	password, err := p.repo.Password(ctx, id)
	if err != nil {
		return "", err
	}
	host := p.opts.InstanceHost
	if host == "" {
		host = "localhost"
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(inst.Superuser, password),
		Host:     fmt.Sprintf("%s:%d", host, inst.HostPort),
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}
	return u.String(), nil
}

// Adopt re-binds an instance to a container discovered by the reconciler.
func (p *Provisioner) Adopt(ctx context.Context, id string, info docker.ContainerInfo) error {
	state, err := p.rt.Inspect(ctx, info.ID)
	if err != nil {
		return err
	}
	hostPort, err := assignedPort(state)
	if err != nil {
		hostPort = 0
	}
	if err := p.repo.SetRuntime(ctx, id, info.ID, hostPort); err != nil {
		return err
	}
	status := instance.StatusStopped
	if state.Running {
		status = instance.StatusRunning
	}
	return p.repo.SetStatus(ctx, id, status, "")
}
