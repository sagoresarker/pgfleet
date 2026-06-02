// Package docker provides a narrow ContainerRuntime abstraction over the
// Docker Engine API. The control plane depends only on this interface, which
// keeps orchestration logic unit-testable (via the in-memory Fake) while the
// moby-backed implementation handles production.
package docker

import (
	"context"
	"io"
)

// Label keys applied to every PgFleet-managed Docker resource.
const (
	LabelManaged  = "pgfleet.managed"  // always "true"
	LabelInstance = "pgfleet.instance" // owning instance id
	LabelRole     = "pgfleet.role"     // e.g. "postgres"
)

// PortMapping maps a container port to a host port.
type PortMapping struct {
	ContainerPort int
	HostPort      int
	Protocol      string // "tcp" (default) or "udp"
}

// Mount binds a named volume to a path inside the container.
type Mount struct {
	Volume string
	Path   string
}

// ContainerSpec describes a container to create.
type ContainerSpec struct {
	Name     string
	Image    string
	Cmd      []string
	Env      map[string]string
	Labels   map[string]string
	Ports    []PortMapping
	Mounts   []Mount
	Networks []string
	User     string
}

// ExecResult is the outcome of an exec inside a container.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ContainerState is a snapshot of a container's runtime state.
type ContainerState struct {
	ID       string
	Name     string
	Status   string // created, running, exited, ...
	Running  bool
	ExitCode int               // valid once Status is "exited"
	Health   string            // "", healthy, unhealthy, starting
	Ports    map[string]string // "5432/tcp" -> host port
}

// ContainerInfo is a lightweight container listing entry.
type ContainerInfo struct {
	ID     string
	Name   string
	State  string
	Labels map[string]string
}

// ContainerRuntime is the orchestration surface the control plane depends on.
type ContainerRuntime interface {
	EnsureImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, spec ContainerSpec) (id string, err error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeoutSeconds *int) error
	RemoveContainer(ctx context.Context, id string, force bool) error
	Inspect(ctx context.Context, id string) (ContainerState, error)
	Exec(ctx context.Context, id string, cmd []string) (ExecResult, error)
	Logs(ctx context.Context, id string, follow bool) (io.ReadCloser, error)
	ListByLabel(ctx context.Context, labels map[string]string) ([]ContainerInfo, error)

	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string, force bool) error
	ListVolumesByLabel(ctx context.Context, labels map[string]string) ([]string, error)

	CreateNetwork(ctx context.Context, name string, labels map[string]string) (id string, err error)
	RemoveNetwork(ctx context.Context, id string) error
}

// labelsMatch reports whether want is a subset of have.
func labelsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
