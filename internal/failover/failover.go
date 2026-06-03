// Package failover provides an in-house automatic-failover controller for HA
// clusters: it detects a dead primary, fences it, promotes the most-caught-up
// replica, reattaches the other replicas, and repoints the PgCat router.
package failover

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/events"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// Promoter is the subset of the provisioner the controller needs.
type Promoter interface {
	PrimaryReachable(ctx context.Context, inst instance.Instance) bool
	ReplayLSN(ctx context.Context, inst instance.Instance) (int64, error)
	Promote(ctx context.Context, inst instance.Instance) error
	Stop(ctx context.Context, id string) error
	ProvisionReplica(ctx context.Context, replicaID string, primary instance.Instance, progress provision.ProgressFunc) error
	StartRouter(ctx context.Context, spec provision.RouterSpec, progress provision.ProgressFunc) (string, int, error)
}

// Clusters / Instances are the repo subsets used.
type Clusters interface {
	List(ctx context.Context) ([]cluster.Cluster, error)
	SetPrimary(ctx context.Context, id, primaryInstanceID string) error
	SetRouter(ctx context.Context, id, containerID string, port int) error
	SetStatus(ctx context.Context, id string, status cluster.Status, lastErr string) error
}

type Instances interface {
	ListByCluster(ctx context.Context, clusterID string) ([]instance.Instance, error)
	Get(ctx context.Context, id string) (instance.Instance, error)
	Password(ctx context.Context, id string) (string, error)
	SetRole(ctx context.Context, id string, role instance.Role) error
	SetStatus(ctx context.Context, id string, status instance.Status, lastErr string) error
}

// RouterRemover removes the old router container during a repoint.
type RouterRemover interface {
	RemoveContainer(ctx context.Context, id string, force bool) error
}

// EventRecorder records durable failover events (optional).
type EventRecorder interface {
	Record(ctx context.Context, ne events.NewEvent) (events.Event, error)
}

// Controller runs failover checks across clusters.
type Controller struct {
	clusters  Clusters
	instances Instances
	prov      Promoter
	router    RouterRemover
	events    EventRecorder
	threshold int
	failures  map[string]int
	log       *slog.Logger
}

// New builds a Controller. threshold is the number of consecutive failed
// primary checks before a failover is triggered (conservative values avoid
// reacting to transient blips). log/events may be nil.
func New(clusters Clusters, instances Instances, prov Promoter, router RouterRemover, ev EventRecorder, threshold int, log *slog.Logger) *Controller {
	if threshold < 1 {
		threshold = 3
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Controller{
		clusters: clusters, instances: instances, prov: prov, router: router,
		events: ev, threshold: threshold, failures: map[string]int{}, log: log,
	}
}

// Run performs one failover pass over all clusters.
func (c *Controller) Run(ctx context.Context) error {
	clusters, err := c.clusters.List(ctx)
	if err != nil {
		return err
	}
	for _, clu := range clusters {
		if clu.Status != cluster.StatusRunning && clu.Status != cluster.StatusDegraded {
			continue
		}
		members, err := c.instances.ListByCluster(ctx, clu.ID)
		if err != nil {
			continue
		}
		primary, replicas := splitMembers(members)

		if primary != nil && c.prov.PrimaryReachable(ctx, *primary) {
			c.failures[clu.ID] = 0
			continue
		}
		c.failures[clu.ID]++
		if c.failures[clu.ID] < c.threshold {
			c.log.Warn("cluster primary unreachable", "cluster", clu.Name, "strikes", c.failures[clu.ID], "threshold", c.threshold)
			continue
		}
		c.failures[clu.ID] = 0
		if err := c.failover(ctx, clu, primary, replicas); err != nil {
			c.log.Warn("failover failed", "cluster", clu.Name, "err", err)
		}
	}
	return nil
}

func splitMembers(members []instance.Instance) (*instance.Instance, []instance.Instance) {
	var primary *instance.Instance
	var replicas []instance.Instance
	for i := range members {
		switch members[i].Role {
		case instance.RolePrimary:
			p := members[i]
			primary = &p
		case instance.RoleReplica:
			replicas = append(replicas, members[i])
		}
	}
	return primary, replicas
}

// elect returns the reachable replica with the highest replayed LSN (most
// caught-up, so the least data loss on promotion).
func elect(ctx context.Context, prov Promoter, replicas []instance.Instance) (instance.Instance, bool) {
	best := instance.Instance{}
	bestLSN := int64(-1)
	found := false
	for _, r := range replicas {
		if !prov.PrimaryReachable(ctx, r) {
			continue
		}
		lsn, err := prov.ReplayLSN(ctx, r)
		if err != nil {
			continue
		}
		if lsn > bestLSN {
			bestLSN, best, found = lsn, r, true
		}
	}
	return best, found
}

func (c *Controller) failover(ctx context.Context, clu cluster.Cluster, oldPrimary *instance.Instance, replicas []instance.Instance) error {
	newP, ok := elect(ctx, c.prov, replicas)
	if !ok {
		_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusError, "primary unreachable and no promotable replica")
		c.record(ctx, clu, "failover aborted: no promotable replica", "")
		return apperr.New(apperr.KindInternal, "failover: no promotable replica")
	}
	c.log.Warn("initiating failover", "cluster", clu.Name, "new_primary", newP.Name)

	// Fence the old primary first (stop its container) so it cannot accept
	// writes after a replica is promoted — prevents split-brain if it was a
	// transient outage rather than a hard failure.
	if oldPrimary != nil {
		_ = c.prov.Stop(ctx, oldPrimary.ID)
	}

	if err := c.prov.Promote(ctx, newP); err != nil {
		_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusError, "promotion failed: "+err.Error())
		return err
	}
	_ = c.instances.SetRole(ctx, newP.ID, instance.RolePrimary)
	_ = c.instances.SetStatus(ctx, newP.ID, instance.StatusRunning, "")
	_ = c.clusters.SetPrimary(ctx, clu.ID, newP.ID)
	if oldPrimary != nil {
		_ = c.instances.SetStatus(ctx, oldPrimary.ID, instance.StatusError, "failed over; rebuild as a replica")
		_ = c.instances.SetRole(ctx, oldPrimary.ID, instance.RoleReplica)
	}

	// Reattach the OTHER replicas by re-cloning from the new primary (they
	// followed the old primary's timeline and would otherwise diverge).
	newPrimary, err := c.instances.Get(ctx, newP.ID)
	if err != nil {
		newPrimary = newP
	}
	degraded := false
	var reattached []string
	for _, r := range replicas {
		if r.ID == newP.ID {
			continue
		}
		if err := c.prov.ProvisionReplica(ctx, r.ID, newPrimary, nil); err != nil {
			degraded = true
			_ = c.instances.SetStatus(ctx, r.ID, instance.StatusError, "reattach failed: "+err.Error())
			continue
		}
		if ri, gerr := c.instances.Get(ctx, r.ID); gerr == nil {
			reattached = append(reattached, ri.Name)
		}
	}

	if err := c.repointRouter(ctx, clu, newPrimary, reattached); err != nil {
		degraded = true
		c.log.Warn("router repoint failed", "cluster", clu.Name, "err", err)
	}

	status, msg := cluster.StatusRunning, ""
	if degraded {
		status, msg = cluster.StatusDegraded, "failed over with degraded members"
	}
	_ = c.clusters.SetStatus(ctx, clu.ID, status, msg)
	c.record(ctx, clu, "failover complete; new primary "+newP.Name, newP.ID)
	return nil
}

func (c *Controller) repointRouter(ctx context.Context, clu cluster.Cluster, newPrimary instance.Instance, replicaNames []string) error {
	password, err := c.instances.Password(ctx, newPrimary.ID)
	if err != nil {
		return err
	}
	adminPassword, err := randomSecret()
	if err != nil {
		return err
	}
	routerID, port, err := c.prov.StartRouter(ctx, provision.RouterSpec{
		ClusterID:     clu.ID,
		ClusterName:   clu.Name,
		Database:      "postgres",
		User:          newPrimary.Superuser,
		Password:      password,
		AdminPassword: adminPassword,
		Members:       provision.RouterMembersFromInstances(newPrimary.Name, replicaNames),
	}, nil)
	if err != nil {
		return err
	}
	if clu.RouterContainerID != "" {
		_ = c.router.RemoveContainer(context.Background(), clu.RouterContainerID, true)
	}
	return c.clusters.SetRouter(ctx, clu.ID, routerID, port)
}

func (c *Controller) record(ctx context.Context, clu cluster.Cluster, message, instanceID string) {
	if c.events == nil {
		return
	}
	_, _ = c.events.Record(ctx, events.NewEvent{
		ClusterID:  clu.ID,
		InstanceID: instanceID,
		Type:       "failover",
		Message:    message,
		Metadata:   map[string]string{"cluster": clu.Name},
	})
}

func randomSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", apperr.Wrap(apperr.KindInternal, "failover: generate secret", err)
	}
	return hex.EncodeToString(b), nil
}
