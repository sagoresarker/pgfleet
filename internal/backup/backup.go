// Package backup runs pgBackRest backups and restores against managed
// instances and keeps a catalog of backups in the meta database.
package backup

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

const confPath = "/etc/pgbackrest/pgbackrest.conf"

// Backup is a catalog row (a parsed backup persisted in the meta database).
type Backup struct {
	ID          string
	InstanceID  string
	Label       string
	Type        string
	RepoSize    int64
	LogicalSize int64
	WALStart    string
	WALStop     string
	StartedAt   time.Time
	StoppedAt   time.Time
	Error       bool
}

// instanceLookup fetches the instance (container id + stanza) to operate on.
type instanceLookup interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

// catalog persists the backup catalog.
type catalog interface {
	Upsert(ctx context.Context, instanceID string, b pgbackrest.BackupInfo) error
	Prune(ctx context.Context, instanceID string, keepLabels []string) error
	List(ctx context.Context, instanceID string) ([]Backup, error)
}

// Service runs backups/restores and syncs the catalog.
type Service struct {
	rt        docker.ContainerRuntime
	instances instanceLookup
	catalog   catalog

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-instance backup serialization
}

// New builds a backup Service.
func New(rt docker.ContainerRuntime, instances instanceLookup, cat catalog) *Service {
	return &Service{rt: rt, instances: instances, catalog: cat, locks: map[string]*sync.Mutex{}}
}

// Run takes a backup of the given type (full|incr|diff), serialized per
// instance, then refreshes the catalog.
func (s *Service) Run(ctx context.Context, instanceID, backupType string) error {
	// Validate the backup type up front, before touching the instance.
	if _, err := pgbackrest.Backup("", confPath, backupType); err != nil {
		return err
	}

	inst, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	// Replicas have no backup stanza; backups run on the cluster primary.
	if inst.Role == instance.RoleReplica {
		return apperr.New(apperr.KindInvalid, "backup: replicas are not backed up directly; back up the cluster primary")
	}

	lock := s.lockFor(instanceID)
	lock.Lock()
	defer lock.Unlock()

	cmd, err := pgbackrest.Backup(inst.Stanza, confPath, backupType)
	if err != nil {
		return err
	}
	if err := s.execOK(ctx, inst.ContainerID, asPostgres(cmd)); err != nil {
		return err
	}
	_, err = s.sync(ctx, inst)
	return err
}

// Sync refreshes the catalog from the live pgBackRest info and returns the
// parsed backups.
func (s *Service) Sync(ctx context.Context, instanceID string) ([]pgbackrest.BackupInfo, error) {
	inst, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return s.sync(ctx, inst)
}

func (s *Service) sync(ctx context.Context, inst instance.Instance) ([]pgbackrest.BackupInfo, error) {
	res, err := s.rt.Exec(ctx, inst.ContainerID, asPostgres(pgbackrest.Info(inst.Stanza, confPath)))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, apperr.New(apperr.KindInternal, "backup: info failed: "+strings.TrimSpace(res.Stderr))
	}
	stanzas, err := pgbackrest.ParseInfo([]byte(res.Stdout))
	if err != nil {
		return nil, err
	}

	var backups []pgbackrest.BackupInfo
	var found bool
	for _, st := range stanzas {
		if st.Name != inst.Stanza {
			continue
		}
		found = true
		backups = st.Backups
	}
	// If the stanza is absent from `info` output entirely, the repo is
	// unreachable/misconfigured rather than empty. Do NOT prune — pruning to an
	// empty keep-set would delete every catalog row for backups that still
	// physically exist, hiding restorable backups during an incident.
	if !found {
		return nil, apperr.New(apperr.KindInternal,
			"backup: stanza "+inst.Stanza+" not present in pgbackrest info; skipping catalog sync")
	}

	labels := make([]string, 0, len(backups))
	for _, b := range backups {
		if err := s.catalog.Upsert(ctx, inst.ID, b); err != nil {
			return nil, err
		}
		labels = append(labels, b.Label)
	}
	if err := s.catalog.Prune(ctx, inst.ID, labels); err != nil {
		return nil, err
	}
	return backups, nil
}

// Expire enforces the configured retention by running pgbackrest expire and
// re-syncing the catalog.
func (s *Service) Expire(ctx context.Context, instanceID string) error {
	inst, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	if err := s.execOK(ctx, inst.ContainerID, asPostgres(pgbackrest.Expire(inst.Stanza, confPath))); err != nil {
		return err
	}
	_, err = s.sync(ctx, inst)
	return err
}

// List returns the catalog for an instance.
func (s *Service) List(ctx context.Context, instanceID string) ([]Backup, error) {
	return s.catalog.List(ctx, instanceID)
}

func (s *Service) lockFor(instanceID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, ok := s.locks[instanceID]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[instanceID] = lock
	}
	return lock
}

func (s *Service) execOK(ctx context.Context, containerID string, cmd []string) error {
	res, err := s.rt.Exec(ctx, containerID, cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return apperr.New(apperr.KindInternal, fmt.Sprintf("backup: command failed (exit %d): %s",
			res.ExitCode, strings.TrimSpace(res.Stderr+res.Stdout)))
	}
	return nil
}

// asPostgres runs a command as the postgres OS user (pgBackRest must not run
// as root).
func asPostgres(cmd []string) []string {
	return append([]string{"gosu", "postgres"}, cmd...)
}
