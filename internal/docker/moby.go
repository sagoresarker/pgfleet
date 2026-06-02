package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/containerd/errdefs"
	"github.com/docker/go-connections/nat"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Moby is a ContainerRuntime backed by the Docker Engine API.
type Moby struct {
	cli *client.Client
}

// NewMoby connects to the Docker daemon using the environment (DOCKER_HOST,
// etc.) with API version negotiation.
func NewMoby() (*Moby, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: connect: %w", err)
	}
	return &Moby{cli: cli}, nil
}

// Close releases the underlying client.
func (m *Moby) Close() error { return m.cli.Close() }

// EnsureImage pulls ref if it is not present locally.
func (m *Moby) EnsureImage(ctx context.Context, ref string) error {
	if _, err := m.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}
	rc, err := m.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker: pull %s: %w", ref, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc) // drain to completion
	return nil
}

func (m *Moby) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	exposed, bindings := portConfig(spec.Ports)

	cfg := &container.Config{
		Image:        spec.Image,
		Cmd:          spec.Cmd,
		Env:          envSlice(spec.Env),
		Labels:       spec.Labels,
		User:         spec.User,
		ExposedPorts: exposed,
	}
	hostCfg := &container.HostConfig{
		PortBindings: bindings,
		Mounts:       mountConfig(spec.Mounts),
	}
	netCfg := networkConfig(spec.Networks)

	resp, err := m.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name)
	if err != nil {
		return "", mapErr("create container", err)
	}
	return resp.ID, nil
}

func (m *Moby) StartContainer(ctx context.Context, id string) error {
	return mapErr("start container", m.cli.ContainerStart(ctx, id, container.StartOptions{}))
}

func (m *Moby) StopContainer(ctx context.Context, id string, timeoutSeconds *int) error {
	return mapErr("stop container", m.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: timeoutSeconds}))
}

func (m *Moby) RemoveContainer(ctx context.Context, id string, force bool) error {
	return mapErr("remove container", m.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: force}))
}

func (m *Moby) Inspect(ctx context.Context, id string) (ContainerState, error) {
	j, err := m.cli.ContainerInspect(ctx, id)
	if err != nil {
		return ContainerState{}, mapErr("inspect container", err)
	}
	st := ContainerState{
		ID:     j.ID,
		Name:   j.Name,
		Ports:  map[string]string{},
	}
	if j.State != nil {
		st.Status = j.State.Status
		st.Running = j.State.Running
		st.ExitCode = j.State.ExitCode
		if j.State.Health != nil {
			st.Health = j.State.Health.Status
		}
	}
	for port, binds := range j.NetworkSettings.Ports {
		if len(binds) > 0 {
			st.Ports[string(port)] = binds[0].HostPort
		}
	}
	return st, nil
}

func (m *Moby) Exec(ctx context.Context, id string, cmd []string) (ExecResult, error) {
	created, err := m.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecResult{}, mapErr("exec create", err)
	}

	att, err := m.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, mapErr("exec attach", err)
	}
	defer att.Close()

	var outBuf, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&outBuf, &errBuf, att.Reader); err != nil {
		return ExecResult{}, fmt.Errorf("docker: read exec output: %w", err)
	}

	insp, err := m.cli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return ExecResult{}, mapErr("exec inspect", err)
	}
	return ExecResult{ExitCode: insp.ExitCode, Stdout: outBuf.String(), Stderr: errBuf.String()}, nil
}

func (m *Moby) Logs(ctx context.Context, id string, follow bool) (io.ReadCloser, error) {
	rc, err := m.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
	})
	if err != nil {
		return nil, mapErr("container logs", err)
	}
	return rc, nil
}

func (m *Moby) ListByLabel(ctx context.Context, labels map[string]string) ([]ContainerInfo, error) {
	list, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: labelFilters(labels),
	})
	if err != nil {
		return nil, mapErr("list containers", err)
	}
	out := make([]ContainerInfo, 0, len(list))
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		out = append(out, ContainerInfo{ID: c.ID, Name: name, State: c.State, Labels: c.Labels})
	}
	return out, nil
}

func (m *Moby) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	_, err := m.cli.VolumeCreate(ctx, volume.CreateOptions{Name: name, Labels: labels})
	return mapErr("create volume", err)
}

func (m *Moby) RemoveVolume(ctx context.Context, name string, force bool) error {
	return mapErr("remove volume", m.cli.VolumeRemove(ctx, name, force))
}

func (m *Moby) ListVolumesByLabel(ctx context.Context, labels map[string]string) ([]string, error) {
	resp, err := m.cli.VolumeList(ctx, volume.ListOptions{Filters: labelFilters(labels)})
	if err != nil {
		return nil, mapErr("list volumes", err)
	}
	names := make([]string, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		names = append(names, v.Name)
	}
	return names, nil
}

func (m *Moby) CreateNetwork(ctx context.Context, name string, labels map[string]string) (string, error) {
	resp, err := m.cli.NetworkCreate(ctx, name, network.CreateOptions{Labels: labels})
	if err != nil {
		return "", mapErr("create network", err)
	}
	return resp.ID, nil
}

func (m *Moby) RemoveNetwork(ctx context.Context, id string) error {
	return mapErr("remove network", m.cli.NetworkRemove(ctx, id))
}

// --- helpers ---

func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func portConfig(ports []PortMapping) (nat.PortSet, nat.PortMap) {
	if len(ports) == 0 {
		return nil, nil
	}
	set := nat.PortSet{}
	binds := nat.PortMap{}
	for _, p := range ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port := nat.Port(strconv.Itoa(p.ContainerPort) + "/" + proto)
		set[port] = struct{}{}
		// HostPort 0 lets Docker assign an ephemeral host port; read it back
		// via Inspect after the container starts.
		hostPort := ""
		if p.HostPort != 0 {
			hostPort = strconv.Itoa(p.HostPort)
		}
		binds[port] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: hostPort}}
	}
	return set, binds
}

func mountConfig(mounts []Mount) []mount.Mount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]mount.Mount, 0, len(mounts))
	for _, mt := range mounts {
		out = append(out, mount.Mount{Type: mount.TypeVolume, Source: mt.Volume, Target: mt.Path})
	}
	return out
}

func networkConfig(networks []string) *network.NetworkingConfig {
	if len(networks) == 0 {
		return nil
	}
	endpoints := map[string]*network.EndpointSettings{}
	for _, n := range networks {
		endpoints[n] = &network.EndpointSettings{}
	}
	return &network.NetworkingConfig{EndpointsConfig: endpoints}
}

func labelFilters(labels map[string]string) filters.Args {
	args := filters.NewArgs()
	for k, v := range labels {
		args.Add("label", k+"="+v)
	}
	return args
}

func mapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return apperr.Wrap(apperr.KindNotFound, "docker: "+op, err)
	}
	return apperr.Wrap(apperr.KindInternal, "docker: "+op, err)
}

// compile-time assertion
var _ ContainerRuntime = (*Moby)(nil)
