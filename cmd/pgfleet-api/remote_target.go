package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sagoresarker/pgfleet/internal/api"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

// remoteTargetProvisioner adapts the existing instance/cluster provisioning to
// the api.RemoteTargetProvisioner interface used by the "migrate-in" (remote
// backup & restore) flow: provision a fresh target, then pg_restore the captured
// remote dump into it.
type remoteTargetProvisioner struct {
	instances  *instance.Repository
	prov       *provision.Provisioner
	clusterSvc *clusterctl.Service
	clusters   *cluster.Repository
}

func newRemoteTargetProvisioner(
	instances *instance.Repository,
	prov *provision.Provisioner,
	clusterSvc *clusterctl.Service,
	clusters *cluster.Repository,
) *remoteTargetProvisioner {
	return &remoteTargetProvisioner{instances: instances, prov: prov, clusterSvc: clusterSvc, clusters: clusters}
}

// ProvisionInstance creates the instance record (status: provisioning) and
// returns its id. The container is actually provisioned in WaitReady so the
// handler can return the id immediately.
func (a *remoteTargetProvisioner) ProvisionInstance(ctx context.Context, spec api.RemoteTargetSpec) (string, error) {
	inst, err := a.instances.Create(ctx, instance.NewInstance{
		Name:       spec.Name,
		PGVersion:  spec.PGVersion,
		RepoType:   instance.RepoType(spec.RepoType),
		Password:   spec.Password,
		Parameters: spec.Parameters,
		Extensions: spec.Extensions,
	})
	if err != nil {
		return "", err
	}
	return inst.ID, nil
}

// ProvisionCluster creates the cluster (and member) records and returns the
// cluster id. Provisioning happens in WaitReady.
func (a *remoteTargetProvisioner) ProvisionCluster(ctx context.Context, spec api.RemoteTargetSpec) (string, error) {
	c, err := a.clusterSvc.Create(ctx, clusterctl.Input{
		Name:       spec.Name,
		Replicas:   spec.Replicas,
		Password:   spec.Password,
		RepoType:   instance.RepoType(spec.RepoType),
		Version:    spec.PGVersion,
		Parameters: spec.Parameters,
		Extensions: spec.Extensions,
	})
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// WaitReady runs the (synchronous) provisioner and, on success, returns the
// superuser DSN to restore into. Provision sets the target's status to running
// on success and error on failure.
func (a *remoteTargetProvisioner) WaitReady(ctx context.Context, kind, id string) (string, error) {
	switch kind {
	case "instance":
		if err := a.prov.Provision(ctx, id, nil); err != nil {
			return "", err
		}
		return a.prov.DSN(ctx, id)
	case "cluster":
		if err := a.clusterSvc.Provision(ctx, id, nil); err != nil {
			return "", err
		}
		// Restore into the PRIMARY instance directly (not the router) so
		// pg_restore speaks straight Postgres; replicas get the data via
		// streaming replication.
		c, err := a.clusters.Get(ctx, id)
		if err != nil {
			return "", err
		}
		if c.PrimaryInstanceID == "" {
			return "", fmt.Errorf("cluster %s has no primary after provisioning", id)
		}
		return a.prov.DSN(ctx, c.PrimaryInstanceID)
	default:
		return "", fmt.Errorf("unknown target kind %q", kind)
	}
}

// MarkError records that the migrate-in restore failed so the target is not left
// silently half-built.
func (a *remoteTargetProvisioner) MarkError(ctx context.Context, kind, id, reason string) {
	// Bound the status write so a cancelled request context cannot wedge it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	switch kind {
	case "instance":
		_ = a.instances.SetStatus(ctx, id, instance.StatusError, reason)
	case "cluster":
		_ = a.clusters.SetStatus(ctx, id, cluster.StatusError, reason)
	}
}
