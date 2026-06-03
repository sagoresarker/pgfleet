package provision

import (
	"context"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/pgcat"
)

// PgCatImage is the query-router image.
const PgCatImage = "ghcr.io/postgresml/pgcat:latest"

const pgcatPort = 6432

// RouterMember is a backend in the router's pool.
type RouterMember struct {
	Host string // container hostname on the cluster network
	Role string // "primary" or "replica"
}

// RouterMirror is an optional shadow target that receives a copy of the
// router's traffic (query mirroring). Host is a container hostname on the
// cluster network.
type RouterMirror struct {
	Host string
	Port int // 0 = default Postgres port
}

// RouterSpec parameterizes a cluster's router.
type RouterSpec struct {
	ClusterID     string
	ClusterName   string
	Database      string
	User          string
	Password      string
	AdminPassword string
	Members       []RouterMember
	// PoolMode selects PgCat pooling: "transaction" (default) or "session".
	// Wired from the cluster's persisted pool_mode (cluster create → provision →
	// re-applied on failover).
	PoolMode string
	// Mirrors are optional query-shadow targets; empty means no mirroring. The
	// config path is fully supported by pgcat.Generate, but mirror targets are
	// not yet surfaced through the cluster API/UI (an operator would need to
	// supply external host:port targets), so callers currently leave this empty.
	// Kept here so enabling it later is a pure wiring change, not a refactor.
	Mirrors []RouterMirror
}

// StartRouter runs a PgCat router fronting the cluster's members and returns
// its container id and assigned host port. The config (with credentials) is
// passed via an env var and written inside the container, never the script.
func (p *Provisioner) StartRouter(ctx context.Context, spec RouterSpec, progress ProgressFunc) (string, int, error) {
	conf := pgcat.Generate(routerConfig(spec))

	progress.emit("router-image", "ensuring router image")
	if err := p.rt.EnsureImage(ctx, PgCatImage); err != nil {
		return "", 0, err
	}

	cspec := docker.ContainerSpec{
		Name:  "pgfleet-router-" + spec.ClusterName,
		Image: PgCatImage,
		Cmd: []string{"sh", "-c",
			`printf '%s' "$PGCAT_CONFIG" > /tmp/pgcat.toml && exec pgcat /tmp/pgcat.toml`},
		Env: map[string]string{"PGCAT_CONFIG": conf},
		Labels: map[string]string{
			docker.LabelManaged:  "true",
			docker.LabelInstance: spec.ClusterID, // group under the cluster
			docker.LabelRole:     "router",
		},
		Ports:         []docker.PortMapping{{ContainerPort: pgcatPort, HostPort: 0, HostIP: p.opts.BindAddress}},
		RestartPolicy: p.opts.RestartPolicy,
	}
	if p.opts.Network != "" {
		cspec.Networks = []string{p.opts.Network}
	}

	progress.emit("router", "starting query router")
	cid, err := p.rt.CreateContainer(ctx, cspec)
	if err != nil {
		return "", 0, err
	}
	// Remove the container on any failure after create, so a router that
	// starts but can't be inspected isn't orphaned (Destroy can't find it
	// because SetRouter is never reached on the error path).
	ok := false
	defer func() {
		if !ok {
			_ = p.rt.RemoveContainer(context.Background(), cid, true)
		}
	}()

	if err := p.rt.StartContainer(ctx, cid); err != nil {
		return "", 0, err
	}
	state, err := p.rt.Inspect(ctx, cid)
	if err != nil {
		return "", 0, err
	}
	port, err := portFor(state, pgcatPort)
	if err != nil {
		return "", 0, err
	}
	ok = true
	return cid, port, nil
}

// routerConfig maps a RouterSpec to the pgcat.Config the router runs with,
// applying the standard Postgres port to members and mirrors. Pure so the
// mapping (pool mode, roles, mirrors) is unit-testable without Docker.
func routerConfig(spec RouterSpec) pgcat.Config {
	servers := make([]pgcat.Server, 0, len(spec.Members))
	for _, m := range spec.Members {
		servers = append(servers, pgcat.Server{Host: m.Host, Port: pgPort, Role: m.Role})
	}
	mirrors := make([]pgcat.MirrorTarget, 0, len(spec.Mirrors))
	for _, m := range spec.Mirrors {
		port := m.Port
		if port == 0 {
			port = pgPort
		}
		mirrors = append(mirrors, pgcat.MirrorTarget{Host: m.Host, Port: port})
	}
	return pgcat.Config{
		ListenPort:    pgcatPort,
		AdminUser:     "pgfleet_admin",
		AdminPassword: spec.AdminPassword,
		Database:      spec.Database,
		User:          spec.User,
		Password:      spec.Password,
		PoolMode:      spec.PoolMode,
		Servers:       servers,
		Mirrors:       mirrors,
	}
}

// RouterMembersFromInstances builds the router pool from cluster instances
// (primary first), using their container names on the shared network.
func RouterMembersFromInstances(primaryName string, replicaNames []string) []RouterMember {
	members := []RouterMember{{Host: InstanceContainerName(primaryName), Role: "primary"}}
	for _, n := range replicaNames {
		members = append(members, RouterMember{Host: InstanceContainerName(n), Role: "replica"})
	}
	return members
}
