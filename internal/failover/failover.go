// Package failover provides an in-house automatic-failover controller for HA
// clusters: it detects a dead primary, fences it, promotes the most-caught-up
// replica, reattaches the other replicas, and repoints the PgCat router.
package failover

import (
	"context"
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
	Fence(ctx context.Context, inst instance.Instance) error
	PrepareReclone(ctx context.Context, inst instance.Instance) error
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
	masterKey []byte
	log       *slog.Logger
}

// New builds a Controller. threshold is the number of consecutive failed
// primary checks before a failover is triggered (conservative values avoid
// reacting to transient blips). masterKey derives the new router's admin
// password (so it stays stable/re-derivable for pool stats). log/events may be
// nil.
func New(clusters Clusters, instances Instances, prov Promoter, router RouterRemover, ev EventRecorder, threshold int, masterKey []byte, log *slog.Logger) *Controller {
	if threshold < 1 {
		threshold = 3
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Controller{
		clusters: clusters, instances: instances, prov: prov, router: router,
		events: ev, threshold: threshold, failures: map[string]int{}, masterKey: masterKey, log: log,
	}
}

// Run performs one failover pass over all clusters.
func (c *Controller) Run(ctx context.Context) error {
	clusters, err := c.clusters.List(ctx)
	if err != nil {
		return err
	}
	live := make(map[string]bool, len(clusters))
	for _, clu := range clusters {
		live[clu.ID] = true
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
	// Prune strike counters for clusters that no longer exist so the map cannot
	// grow unbounded over the control plane's lifetime.
	for id := range c.failures {
		if !live[id] {
			delete(c.failures, id)
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
	bestLSN := int64(0) // a standby that has replayed nothing (lsn 0) is not promotable
	found := false
	for _, r := range replicas {
		if !prov.PrimaryReachable(ctx, r) {
			continue
		}
		lsn, err := prov.ReplayLSN(ctx, r)
		if err != nil || lsn <= 0 {
			continue
		}
		if lsn > bestLSN {
			bestLSN, best, found = lsn, r, true
		}
	}
	return best, found
}

func (c *Controller) failover(ctx context.Context, clu cluster.Cluster, oldPrimary *instance.Instance, replicas []instance.Instance) error {
	// Quorum / witness guard (split-brain prevention). The controller only
	// reaches a failover when it cannot see the primary. But the primary may
	// actually be alive on the far side of a network partition with the
	// controller stranded on the MINORITY side. Promoting then yields two live
	// primaries (split-brain). So before promoting, require that the members the
	// controller CAN currently reach form a strict majority of total membership.
	//
	// Total membership = len(replicas) + 1 (the primary). In a failover the
	// primary is unreachable, so it contributes 0; only the reachable replicas
	// count toward quorum. Rule: reachableReplicas >= floor(total/2) + 1.
	//
	//   3-node (1p+2r, total=3, majority=2): need BOTH replicas reachable.
	//     Only 1 reachable (1 < 2) => likely minority side => abort.
	//   2-node (1p+1r, total=2, majority=2): need that 1 replica reachable. It
	//     is also the only promotion candidate, so this does not deadlock a
	//     legitimate 2-node failover. CAVEAT: a 2-node cluster cannot truly
	//     avoid split-brain on a partition without an external witness/arbiter —
	//     if the lone replica is reachable but the primary is alive-but-isolated,
	//     we will still promote. Add a third node (or a witness) for real safety.
	total := len(replicas) + 1
	majority := total/2 + 1
	reachable := 0
	for _, r := range replicas {
		if c.prov.PrimaryReachable(ctx, r) {
			reachable++
		}
	}
	// 2-node exception: with total=2 the majority is 2, but the primary is down
	// (contributes 0) and there is exactly one replica, so the majority is
	// mathematically unreachable from replicas alone. Requiring it would deadlock
	// every legitimate 2-node failover. We therefore allow promotion when that
	// sole replica is reachable. This is the documented 2-node caveat above: a
	// 2-node cluster cannot truly avoid split-brain on a partition without an
	// external witness. For total >= 3 the strict-majority rule applies normally.
	needed := majority
	if len(replicas) == 1 {
		needed = 1
	}
	if reachable < needed {
		msg := "quorum not met — refusing to promote (possible partition)"
		_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusError, msg)
		c.record(ctx, clu, "failover aborted: "+msg, "")
		c.log.Warn("failover aborted: quorum not met",
			"cluster", clu.Name, "reachable_replicas", reachable, "majority", majority, "total_members", total)
		return apperr.New(apperr.KindInternal, "failover: "+msg)
	}

	newP, ok := elect(ctx, c.prov, replicas)
	if !ok {
		_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusError, "primary unreachable and no promotable replica")
		c.record(ctx, clu, "failover aborted: no promotable replica", "")
		return apperr.New(apperr.KindInternal, "failover: no promotable replica")
	}
	c.log.Warn("initiating failover", "cluster", clu.Name, "new_primary", newP.Name)

	// Fence the old primary FIRST — stop AND remove its container so it cannot
	// accept writes (and cannot be revived by its restart policy) after a
	// replica is promoted. If fencing fails we cannot guarantee the old primary
	// is down, so we abort rather than risk split-brain (two writable primaries).
	if oldPrimary != nil {
		if err := c.prov.Fence(ctx, *oldPrimary); err != nil {
			_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusError, "failover aborted: could not fence old primary: "+err.Error())
			c.record(ctx, clu, "failover aborted: fencing the old primary failed", "")
			return err
		}
	}

	if err := c.prov.Promote(ctx, newP); err != nil {
		_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusError, "promotion failed: "+err.Error())
		return err
	}
	// Physical promotion succeeded (irreversible). These writes record WHICH node
	// is now primary; if they fail, the meta DB still points at the old primary —
	// a dangerous mismatch (a later failover pass would try to fence the wrong
	// node and re-promote). Can't roll back the promotion, so surface it loudly
	// and mark the cluster degraded for operator attention.
	roleErr := c.instances.SetRole(ctx, newP.ID, instance.RolePrimary)
	_ = c.instances.SetStatus(ctx, newP.ID, instance.StatusRunning, "")
	primErr := c.clusters.SetPrimary(ctx, clu.ID, newP.ID)
	if roleErr != nil || primErr != nil {
		c.log.Error("failover: promoted new primary but failed to persist new-primary metadata",
			"cluster", clu.ID, "new_primary", newP.ID, "set_role_err", roleErr, "set_primary_err", primErr)
		_ = c.clusters.SetStatus(ctx, clu.ID, cluster.StatusDegraded,
			"promoted "+newP.Name+" but failed to update primary metadata; manual check needed")
	}
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
		// Mark provisioning BEFORE removing the container to reclone. The
		// reconciler skips StatusProvisioning, so a 30s reconcile tick during the
		// (possibly minutes-long) basebackup won't see a container-less
		// StatusRunning replica and wrongly flip it to "error" mid-failover.
		_ = c.instances.SetStatus(ctx, r.ID, instance.StatusProvisioning, "")
		// Wipe the old container + data volume first; ProvisionReplica needs an
		// empty data volume and a free container name (the replica followed the
		// old, now-fenced primary, so its data is stale anyway).
		if err := c.prov.PrepareReclone(ctx, r); err != nil {
			degraded = true
			_ = c.instances.SetStatus(ctx, r.ID, instance.StatusError, "reattach prep failed: "+err.Error())
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
	// Derive the same admin password clusterctl uses, so live pool stats keep
	// working after the router is repointed to the new primary.
	adminPassword := provision.RouterAdminPass(c.masterKey, clu.ID)
	routerID, port, err := c.prov.StartRouter(ctx, provision.RouterSpec{
		ClusterID:     clu.ID,
		ClusterName:   clu.Name,
		Database:      "postgres",
		User:          newPrimary.Superuser,
		Password:      password,
		AdminPassword: adminPassword,
		// Re-apply the cluster's persisted pool mode so a failover doesn't
		// silently revert the router to the default (N8).
		PoolMode: clu.PoolMode,
		Members:  provision.RouterMembersFromInstances(newPrimary.Name, replicaNames),
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
