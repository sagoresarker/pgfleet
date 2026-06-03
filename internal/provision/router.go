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

// RouterSpec parameterizes a cluster's router.
type RouterSpec struct {
	ClusterID     string
	ClusterName   string
	Database      string
	User          string
	Password      string
	AdminPassword string
	Members       []RouterMember
}

// StartRouter runs a PgCat router fronting the cluster's members and returns
// its container id and assigned host port. The config (with credentials) is
// passed via an env var and written inside the container, never the script.
func (p *Provisioner) StartRouter(ctx context.Context, spec RouterSpec, progress ProgressFunc) (string, int, error) {
	servers := make([]pgcat.Server, 0, len(spec.Members))
	for _, m := range spec.Members {
		servers = append(servers, pgcat.Server{Host: m.Host, Port: pgPort, Role: m.Role})
	}
	conf := pgcat.Generate(pgcat.Config{
		ListenPort:    pgcatPort,
		AdminUser:     "pgfleet_admin",
		AdminPassword: spec.AdminPassword,
		Database:      spec.Database,
		User:          spec.User,
		Password:      spec.Password,
		Servers:       servers,
	})

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
		Ports:         []docker.PortMapping{{ContainerPort: pgcatPort, HostPort: 0}},
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

// RouterMembersFromInstances builds the router pool from cluster instances
// (primary first), using their container names on the shared network.
func RouterMembersFromInstances(primaryName string, replicaNames []string) []RouterMember {
	members := []RouterMember{{Host: InstanceContainerName(primaryName), Role: "primary"}}
	for _, n := range replicaNames {
		members = append(members, RouterMember{Host: InstanceContainerName(n), Role: "replica"})
	}
	return members
}
