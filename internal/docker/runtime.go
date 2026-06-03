// Package docker provides a narrow ContainerRuntime abstraction over the
// Docker Engine API. The control plane depends only on this interface, which
// keeps orchestration logic unit-testable (via the in-memory Fake) while the
// moby-backed implementation handles production.
package docker

import (
	"context"
	"io"
)

// Label keys applied to every PgFleet-managed Docker resource. The recovery
// labels (name/stanza/repo/version/cluster) carry non-secret metadata so an
// instance is identifiable and reconstructible from Docker alone if the meta
// DB is lost.
const (
	LabelManaged   = "pgfleet.managed"  // always "true"
	LabelInstance  = "pgfleet.instance" // owning instance id
	LabelRole      = "pgfleet.role"     // e.g. "postgres", "replica", "router"
	LabelName      = "pgfleet.name"     // instance name
	LabelStanza    = "pgfleet.stanza"   // pgBackRest stanza (== name)
	LabelRepoType  = "pgfleet.repo"     // "s3" | "local"
	LabelPGVersion = "pgfleet.pgversion"
	LabelCluster   = "pgfleet.cluster" // owning cluster id, if any
)

// PortMapping maps a container port to a host port.
type PortMapping struct {
	ContainerPort int
	HostPort      int
	Protocol      string // "tcp" (default) or "udp"
	// HostIP is the host interface the published port binds to. Empty defaults
	// to 0.0.0.0 (all interfaces); set to 127.0.0.1 or a private IP to limit
	// exposure of the managed database.
	HostIP string
}

// Mount binds a named volume to a path inside the container.
type Mount struct {
	Volume string
	Path   string
	// ReadOnly mounts the volume read-only. Used so a clone's restore container
	// cannot write into the SOURCE instance's live backup repo.
	ReadOnly bool
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
	// RestartPolicy is the Docker restart policy (e.g. "unless-stopped",
	// "always", "no"). Empty means the daemon default ("no").
	RestartPolicy string
}

// ExecResult is the outcome of an exec inside a container.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ContainerState is a snapshot of a container's runtime state.
type ContainerState struct {
	ID            string
	Name          string
	Status        string // created, running, exited, ...
	Running       bool
	ExitCode      int               // valid once Status is "exited"
	Health        string            // "", healthy, unhealthy, starting
	Ports         map[string]string // "5432/tcp" -> host port
	RestartPolicy string            // configured restart policy, if any
}

// ContainerStats is a point-in-time resource-usage snapshot for a container.
//
// The DiskIO* fields carry cumulative block-I/O counters summed from the
// runtime's recursive blkio stats (Docker's BlkioStats). They are lifetime
// totals (monotonic counters), not rates; the rollup layer computes rates by
// deltaing successive samples. DiskIOAvailable reports whether the runtime
// actually surfaced blkio data: on some platforms (notably Docker Desktop on
// macOS, and some cgroup v2 setups) the recursive blkio lists come back empty,
// in which case the counters are meaningless zeros and must not be emitted as
// if they were real measurements. Callers must check DiskIOAvailable before
// using any DiskIO* field.
type ContainerStats struct {
	CPUPercent       float64
	MemoryBytes      int64
	MemoryLimitBytes int64
	MemoryPercent    float64

	// DiskIOAvailable is true only when the runtime returned non-empty blkio
	// stats and the DiskIO* counters below are meaningful.
	DiskIOAvailable bool
	DiskReadBytes   uint64
	DiskWriteBytes  uint64
	DiskReadOps     uint64
	DiskWriteOps    uint64
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
	ContainerStats(ctx context.Context, id string) (ContainerStats, error)
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
