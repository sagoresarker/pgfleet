// Package clusterctl orchestrates high-availability clusters: it provisions a
// primary, its replicas, and a PgCat router, and tears them down together.
package clusterctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// randomSecret returns a 32-hex-char random secret (for the router admin user).
func randomSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", apperr.Wrap(apperr.KindInternal, "cluster: generate secret", err)
	}
	return hex.EncodeToString(b), nil
}

// Provisioner is the subset of the provisioner the orchestrator needs.
type Provisioner interface {
	Provision(ctx context.Context, id string, progress provision.ProgressFunc) error
	ProvisionReplica(ctx context.Context, replicaID string, primary instance.Instance, progress provision.ProgressFunc) error
	StartRouter(ctx context.Context, spec provision.RouterSpec, progress provision.ProgressFunc) (string, int, error)
	Destroy(ctx context.Context, id string, retainBackups bool) error
	DropReplicationSlot(ctx context.Context, primary instance.Instance, slot string) error
}

// instanceStore is the subset of the instance repo the orchestrator needs.
type instanceStore interface {
	Create(ctx context.Context, in instance.NewInstance) (instance.Instance, error)
	Get(ctx context.Context, id string) (instance.Instance, error)
	Password(ctx context.Context, id string) (string, error)
	ListByCluster(ctx context.Context, clusterID string) ([]instance.Instance, error)
}

// clusterStore is the subset of the cluster repo the orchestrator needs.
type clusterStore interface {
	Create(ctx context.Context, in cluster.NewCluster) (cluster.Cluster, error)
	Get(ctx context.Context, id string) (cluster.Cluster, error)
	SetPrimary(ctx context.Context, id, primaryInstanceID string) error
	SetRouter(ctx context.Context, id, containerID string, port int) error
	SetStatus(ctx context.Context, id string, status cluster.Status, lastErr string) error
	Delete(ctx context.Context, id string) error
}

// Service orchestrates cluster lifecycle.
type Service struct {
	clusters  clusterStore
	instances instanceStore
	prov      Provisioner
	router    RouterRemover
	repoType  instance.RepoType
}

// RouterRemover removes a router container (the runtime).
type RouterRemover interface {
	RemoveContainer(ctx context.Context, id string, force bool) error
}

// New builds a cluster Service.
func New(clusters clusterStore, instances instanceStore, prov Provisioner, router RouterRemover, defaultRepo instance.RepoType) *Service {
	return &Service{clusters: clusters, instances: instances, prov: prov, router: router, repoType: defaultRepo}
}

// Input describes a cluster to create.
type Input struct {
	Name     string
	Replicas int
	Password string
	RepoType instance.RepoType
}

// Create validates input and persists the cluster + member instance rows
// (primary + replicas) in "provisioning" state. Provisioning the containers is
// done asynchronously via Provision.
func (s *Service) Create(ctx context.Context, in Input) (cluster.Cluster, error) {
	if err := (cluster.NewCluster{Name: in.Name}).Validate(); err != nil {
		return cluster.Cluster{}, err
	}
	if in.Replicas < 0 || in.Replicas > 9 {
		return cluster.Cluster{}, apperr.New(apperr.KindInvalid, "cluster: replicas must be 0-9")
	}
	repoType := in.RepoType
	if repoType == "" {
		repoType = s.repoType
	}

	c, err := s.clusters.Create(ctx, cluster.NewCluster{Name: in.Name})
	if err != nil {
		return cluster.Cluster{}, err
	}

	// Primary.
	if _, err := s.instances.Create(ctx, instance.NewInstance{
		Name: in.Name + "-p", RepoType: repoType, Password: in.Password,
		ClusterID: c.ID, Role: instance.RolePrimary,
	}); err != nil {
		_ = s.clusters.SetStatus(ctx, c.ID, cluster.StatusError, err.Error())
		return cluster.Cluster{}, err
	}
	// Replicas.
	for i := 1; i <= in.Replicas; i++ {
		if _, err := s.instances.Create(ctx, instance.NewInstance{
			Name: fmt.Sprintf("%s-r%d", in.Name, i), RepoType: repoType, Password: in.Password,
			ClusterID: c.ID, Role: instance.RoleReplica,
		}); err != nil {
			_ = s.clusters.SetStatus(ctx, c.ID, cluster.StatusError, err.Error())
			return cluster.Cluster{}, err
		}
	}
	return c, nil
}

// Provision provisions the cluster's containers: the primary, then each
// replica (streaming from the primary), then the router. It records the
// primary, router, and final status. Intended to run in a background goroutine.
func (s *Service) Provision(ctx context.Context, clusterID string, progress provision.ProgressFunc) error {
	if err := s.provision(ctx, clusterID, progress); err != nil {
		_ = s.clusters.SetStatus(ctx, clusterID, cluster.StatusError, err.Error())
		return err
	}
	return s.clusters.SetStatus(ctx, clusterID, cluster.StatusRunning, "")
}

func (s *Service) provision(ctx context.Context, clusterID string, progress provision.ProgressFunc) error {
	c, err := s.clusters.Get(ctx, clusterID)
	if err != nil {
		return err
	}
	members, err := s.instances.ListByCluster(ctx, clusterID)
	if err != nil {
		return err
	}
	if len(members) == 0 || members[0].Role != instance.RolePrimary {
		return apperr.New(apperr.KindInternal, "cluster: no primary member")
	}
	primary := members[0]
	replicas := members[1:]

	emit(progress, "primary", "provisioning primary")
	if err := s.prov.Provision(ctx, primary.ID, progress); err != nil {
		return err
	}
	primary, _ = s.instances.Get(ctx, primary.ID)
	if err := s.clusters.SetPrimary(ctx, clusterID, primary.ID); err != nil {
		return err
	}

	replicaNames := make([]string, 0, len(replicas))
	for _, r := range replicas {
		emit(progress, "replica", "provisioning replica "+r.Name)
		if err := s.prov.ProvisionReplica(ctx, r.ID, primary, progress); err != nil {
			return err
		}
		replicaNames = append(replicaNames, r.Name)
	}

	password, err := s.instances.Password(ctx, primary.ID)
	if err != nil {
		return err
	}
	adminPassword, err := randomSecret()
	if err != nil {
		return err
	}
	routerID, port, err := s.prov.StartRouter(ctx, provision.RouterSpec{
		ClusterID:   c.ID,
		ClusterName: c.Name,
		Database:    "postgres",
		User:        primary.Superuser,
		Password:    password,
		// Independent random admin password so a router-admin leak is not a
		// database-superuser leak (and vice versa).
		AdminPassword: adminPassword,
		Members:       provision.RouterMembersFromInstances(primary.Name, replicaNames),
	}, progress)
	if err != nil {
		return err
	}
	return s.clusters.SetRouter(ctx, clusterID, routerID, port)
}

// ConnectionDSN builds the client connection string for a cluster: it targets
// the router, which transparently splits reads/writes across the members.
func (s *Service) ConnectionDSN(ctx context.Context, clusterID, host string) (string, error) {
	c, err := s.clusters.Get(ctx, clusterID)
	if err != nil {
		return "", err
	}
	if c.RouterPort == 0 {
		return "", apperr.New(apperr.KindNotFound, "cluster: router not ready")
	}
	members, err := s.instances.ListByCluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	if len(members) == 0 {
		return "", apperr.New(apperr.KindInternal, "cluster: no members")
	}
	primary := members[0]
	password, err := s.instances.Password(ctx, primary.ID)
	if err != nil {
		return "", err
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(primary.Superuser, password),
		Host:     fmt.Sprintf("%s:%d", host, c.RouterPort),
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}
	return u.String(), nil
}

func emit(p provision.ProgressFunc, step, detail string) {
	if p != nil {
		p(step, detail)
	}
}

// Destroy tears down the router and all member instances, then deletes the
// cluster row.
func (s *Service) Destroy(ctx context.Context, clusterID string, retainBackups bool) error {
	c, err := s.clusters.Get(ctx, clusterID)
	if err != nil {
		return err
	}
	_ = s.clusters.SetStatus(ctx, clusterID, cluster.StatusDestroying, "")

	if c.RouterContainerID != "" {
		_ = s.router.RemoveContainer(context.Background(), c.RouterContainerID, true)
	}
	members, err := s.instances.ListByCluster(ctx, clusterID)
	if err != nil {
		return err
	}
	// ListByCluster returns the primary first. Destroy replicas FIRST (dropping
	// each one's slot on the still-alive primary so no WAL stays pinned), and
	// the primary LAST.
	var primary instance.Instance
	if len(members) > 0 && members[0].Role == instance.RolePrimary {
		primary = members[0]
	}
	for i := len(members) - 1; i >= 0; i-- {
		m := members[i]
		if m.Role == instance.RoleReplica && primary.ContainerID != "" {
			_ = s.prov.DropReplicationSlot(ctx, primary, provision.SlotName(m.Name))
		}
		if err := s.prov.Destroy(ctx, m.ID, retainBackups); err != nil {
			_ = s.clusters.SetStatus(ctx, clusterID, cluster.StatusError, "destroy failed: "+err.Error())
			return err
		}
	}
	return s.clusters.Delete(ctx, clusterID)
}
