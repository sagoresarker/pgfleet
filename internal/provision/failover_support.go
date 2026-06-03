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
