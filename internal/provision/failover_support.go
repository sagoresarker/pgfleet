package provision

import (
	"context"
	"strconv"
	"strings"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// PrimaryReachable reports whether the instance's Postgres accepts connections.
// Used by the failover controller to detect a dead primary.
func (p *Provisioner) PrimaryReachable(ctx context.Context, inst instance.Instance) bool {
	if inst.ContainerID == "" {
		return false
	}
	res, err := p.rt.Exec(ctx, inst.ContainerID, asPostgres([]string{"pg_isready", "-U", inst.Superuser}))
	return err == nil && res.ExitCode == 0
}

// ReplayLSN returns a standby's last-replayed WAL position as a monotonically
// comparable integer (bytes from 0/0), so the failover controller can elect the
// most-caught-up replica. A standby that has replayed more is more eligible.
func (p *Provisioner) ReplayLSN(ctx context.Context, inst instance.Instance) (int64, error) {
	if inst.ContainerID == "" {
		return 0, apperr.New(apperr.KindInternal, "failover: replica has no container")
	}
	res, err := p.rt.Exec(ctx, inst.ContainerID, asPostgres([]string{
		"psql", "-U", inst.Superuser, "-d", "postgres", "-tAc",
		"SELECT COALESCE(pg_wal_lsn_diff(pg_last_wal_replay_lsn(), '0/0'), 0)::bigint",
	}))
	if err != nil || res.ExitCode != 0 {
		return 0, apperr.New(apperr.KindInternal, "failover: query replay lsn: "+strings.TrimSpace(res.Stderr))
	}
	n, perr := strconv.ParseInt(strings.TrimSpace(res.Stdout), 10, 64)
	if perr != nil {
		return 0, apperr.Wrap(apperr.KindInternal, "failover: parse replay lsn", perr)
	}
	return n, nil
}

// Promote promotes a standby to a primary (advancing to a new timeline). It
// waits for the promotion to complete.
func (p *Provisioner) Promote(ctx context.Context, inst instance.Instance) error {
	if inst.ContainerID == "" {
		return apperr.New(apperr.KindInternal, "failover: instance has no container")
	}
	return p.execOK(ctx, inst.ContainerID, asPostgres([]string{
		"psql", "-U", inst.Superuser, "-d", "postgres", "-tAc", "SELECT pg_promote(wait := true)",
	}))
}

// Fence permanently removes a failed primary's container (not just Stop — a
// stopped container with RestartPolicy=unless-stopped could be revived and
// cause split-brain). The data volume and the instance row are kept so an
// operator can rebuild it as a replica. Returns an error if the container could
// not be removed (the caller must NOT promote a replica when fencing fails).
func (p *Provisioner) Fence(ctx context.Context, inst instance.Instance) error {
	if inst.ContainerID == "" {
		return nil // nothing running to fence
	}
	timeout := stopTimeoutSeconds
	_ = p.rt.StopContainer(ctx, inst.ContainerID, &timeout)
	if err := p.rt.RemoveContainer(ctx, inst.ContainerID, true); err != nil {
		return apperr.Wrap(apperr.KindInternal, "failover: fence old primary", err)
	}
	_ = p.repo.SetRuntime(ctx, inst.ID, "", 0)
	return nil
}

// PrepareReclone removes a replica's container and stale data volume so it can
// be re-cloned cleanly from the new primary (ProvisionReplica needs an empty
// data volume and a free container name). The repo volume and row are kept.
func (p *Provisioner) PrepareReclone(ctx context.Context, inst instance.Instance) error {
	if inst.ContainerID != "" {
		timeout := stopTimeoutSeconds
		_ = p.rt.StopContainer(ctx, inst.ContainerID, &timeout)
		_ = p.rt.RemoveContainer(ctx, inst.ContainerID, true)
	}
	dataVol := inst.DataVolume
	if dataVol == "" {
		dataVol = volumeName("data", inst.ID)
	}
	if err := p.rt.RemoveVolume(ctx, dataVol, true); err != nil {
		return apperr.Wrap(apperr.KindInternal, "failover: reset replica data volume", err)
	}
	_ = p.repo.SetRuntime(ctx, inst.ID, "", 0)
	return nil
}
