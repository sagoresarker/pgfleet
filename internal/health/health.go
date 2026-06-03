// Package health assesses the reliability posture of managed instances: WAL
// archiving health, backup freshness, pg_wal pressure, and (optionally)
// whether the latest backup actually restores.
package health

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

const (
	confPath   = "/etc/pgbackrest/pgbackrest.conf"
	pgDataPath = "/var/lib/postgresql/data"
)

// Report is the health assessment for one instance.
type Report struct {
	InstanceID    string        `json:"instance_id"`
	ArchivingOK   bool          `json:"archiving_ok"`
	HasBackup     bool          `json:"has_backup"`
	LastBackupAge time.Duration `json:"-"`
	WALBytes      int64         `json:"wal_bytes"`
	DrillRan      bool          `json:"drill_ran"`
	DrillOK       bool          `json:"drill_ok"`
	Issues        []string      `json:"issues"`
	CheckedAt     time.Time     `json:"checked_at"`
}

// Healthy reports whether the instance has no outstanding issues.
func (r Report) Healthy() bool { return len(r.Issues) == 0 }

// Thresholds bound acceptable backup age and pg_wal size.
type Thresholds struct {
	MaxBackupAge time.Duration
	MaxWALBytes  int64
}

// DefaultThresholds returns sensible defaults (25h backup age, 2 GiB pg_wal).
func DefaultThresholds() Thresholds {
	return Thresholds{MaxBackupAge: 25 * time.Hour, MaxWALBytes: 2 << 30}
}

type instanceLookup interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

type backupLister interface {
	List(ctx context.Context, instanceID string) ([]backup.Backup, error)
}

// Checker assesses instance health.
type Checker struct {
	rt         docker.ContainerRuntime
	instances  instanceLookup
	catalog    backupLister
	thresholds Thresholds
	now        func() time.Time
}

// NewChecker builds a Checker.
func NewChecker(rt docker.ContainerRuntime, instances instanceLookup, catalog backupLister, th Thresholds) *Checker {
	return &Checker{rt: rt, instances: instances, catalog: catalog, thresholds: th, now: time.Now}
}

// Check assesses one instance and returns its report.
func (c *Checker) Check(ctx context.Context, instanceID string) (Report, error) {
	inst, err := c.instances.Get(ctx, instanceID)
	if err != nil {
		return Report{}, err
	}

	r := Report{InstanceID: instanceID, CheckedAt: c.now(), Issues: []string{}}

	// WAL archiving health.
	if res, err := c.rt.Exec(ctx, inst.ContainerID, asPostgres(pgbackrest.Check(inst.Stanza, confPath))); err == nil && res.ExitCode == 0 {
		r.ArchivingOK = true
	} else {
		r.Issues = append(r.Issues, "WAL archiving check is failing")
	}

	// Backup freshness.
	backups, err := c.catalog.List(ctx, instanceID)
	if err != nil {
		return Report{}, err
	}
	if len(backups) == 0 {
		r.Issues = append(r.Issues, "no backups exist")
	} else {
		r.HasBackup = true
		// Find the newest *completed* backup. A row with a zero StoppedAt never
		// finished; using it would make the age read as decades old.
		var newest time.Time
		for _, b := range backups {
			if b.StoppedAt.After(newest) {
				newest = b.StoppedAt
			}
		}
		if newest.IsZero() {
			r.Issues = append(r.Issues, "no completed backup yet")
		} else {
			r.LastBackupAge = c.now().Sub(newest)
			if r.LastBackupAge > c.thresholds.MaxBackupAge {
				r.Issues = append(r.Issues, fmt.Sprintf("last backup is %s old", r.LastBackupAge.Round(time.Minute)))
			}
		}
	}

	// pg_wal pressure. A failed probe must NOT be mistaken for "no pressure";
	// flag it as an issue so a broken instance is never reported healthy.
	if res, err := c.rt.Exec(ctx, inst.ContainerID, []string{"du", "-sb", pgDataPath + "/pg_wal"}); err == nil && res.ExitCode == 0 {
		r.WALBytes = parseDuBytes(res.Stdout)
		if c.thresholds.MaxWALBytes > 0 && r.WALBytes > c.thresholds.MaxWALBytes {
			r.Issues = append(r.Issues, "pg_wal is large; archiving may be stalled")
		}
	} else {
		r.Issues = append(r.Issues, "pg_wal probe failed")
	}

	return r, nil
}

// parseDuBytes extracts the byte count from `du -sb` output. Overflow saturates
// to MaxInt64 (so an absurdly large disk still trips the pressure threshold
// rather than silently reading as zero); malformed or negative values clamp to
// 0.
func parseDuBytes(out string) int64 {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(fields[0], "-") {
			return math.MaxInt64
		}
		return 0
	}
	if n < 0 {
		return 0
	}
	return n
}

func asPostgres(cmd []string) []string {
	return append([]string{"gosu", "postgres"}, cmd...)
}
