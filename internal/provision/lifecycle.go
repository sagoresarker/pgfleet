package provision

import (
	"context"
	"fmt"
	"net/url"

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
// repository volume unless retainBackups is set, then deletes its row.
func (p *Provisioner) Destroy(ctx context.Context, id string, retainBackups bool) error {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	_ = p.repo.SetStatus(ctx, id, instance.StatusDestroying, "")

	if inst.ContainerID != "" {
		if err := p.rt.RemoveContainer(ctx, inst.ContainerID, true); err != nil {
			return err
		}
	}
	if err := p.rt.RemoveVolume(ctx, volumeName("data", id), true); err != nil {
		return err
	}
	if !retainBackups && inst.RepoType == instance.RepoLocal {
		if err := p.rt.RemoveVolume(ctx, volumeName("repo", id), true); err != nil {
			return err
		}
	}
	return p.repo.Delete(ctx, id)
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
