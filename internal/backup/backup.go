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
	"github.com/sagoresarker/pgfleet/internal/events"
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
	// Annotations are the user-supplied key/value pairs stored on the backup set
	// (e.g. "name"). Surfaced from pgBackRest info via Sync. Persisting these to
	// the meta DB requires a migration (see advanced_backup_wiring.md); until
	// then they are populated only on the in-memory BackupInfo, not the catalog.
	Annotations map[string]string
}

// instanceLookup fetches the instance (container id + stanza) to operate on.
type instanceLookup interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

// catalog persists the backup catalog.
type catalog interface {
	Upsert(ctx context.Context, instanceID string, b pgbackrest.BackupInfo) error
	Prune(ctx context.Context, instanceID string, keepLabels []string) error
	Delete(ctx context.Context, instanceID, label string) error
	List(ctx context.Context, instanceID string) ([]Backup, error)
}

// EventRecorder records durable backup lifecycle events (optional). It mirrors
// the failover controller's recorder so the same events.Store satisfies both.
type EventRecorder interface {
	Record(ctx context.Context, ne events.NewEvent) (events.Event, error)
}

// Service runs backups/restores and syncs the catalog.
type Service struct {
	rt        docker.ContainerRuntime
	instances instanceLookup
	catalog   catalog
	events    EventRecorder

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-instance backup serialization
}

// New builds a backup Service.
func New(rt docker.ContainerRuntime, instances instanceLookup, cat catalog) *Service {
	return &Service{rt: rt, instances: instances, catalog: cat, locks: map[string]*sync.Mutex{}}
}

// WithEvents attaches a durable event recorder. Passing nil disables recording
// (the Service is a no-op recorder by default), keeping callers that do not wire
// events unaffected. Returns the Service for chaining.
func (s *Service) WithEvents(rec EventRecorder) *Service {
	s.events = rec
	return s
}

// RunOpts parameterizes a backup beyond its type.
type RunOpts struct {
	// Annotation, when non-empty, names the backup: it is stored on the backup
	// set as the "name" annotation (--annotation=name=<value>) and surfaced back
	// in the catalog/info so the UI can display it.
	Annotation string
	// Standby, when true, takes the backup from a standby (replica) to offload
	// the primary (--backup-standby). It only takes effect when the stanza has a
	// reachable standby configured; otherwise pgBackRest uses the primary.
	Standby bool
}

// Run takes a backup of the given type (full|incr|diff), serialized per
// instance, then refreshes the catalog.
func (s *Service) Run(ctx context.Context, instanceID, backupType string) error {
	return s.RunWith(ctx, instanceID, backupType, RunOpts{})
}

// RunWith is Run with extra backup options (annotation, standby offload).
func (s *Service) RunWith(ctx context.Context, instanceID, backupType string, o RunOpts) error {
	opts := pgbackrest.BackupOpts{BackupStandby: o.Standby}
	if o.Annotation != "" {
		opts.Annotations = map[string]string{"name": o.Annotation}
	}

	// Validate the backup type up front, before touching the instance.
	if _, err := pgbackrest.Backup("", confPath, backupType, opts); err != nil {
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

	cmd, err := pgbackrest.Backup(inst.Stanza, confPath, backupType, opts)
	if err != nil {
		return err
	}
	s.record(ctx, inst, "backup started ("+backupType+")", backupType, "")
	if err := s.execOK(ctx, inst.ContainerID, asPostgres(cmd)); err != nil {
		return err
	}
	if _, err := s.sync(ctx, inst); err != nil {
		return err
	}
	s.record(ctx, inst, "backup completed ("+backupType+")", backupType, "")
	return nil
}

// Delete removes a single backup set identified by its label: it runs
// pgbackrest expire --set=<label> against the instance's stanza, then removes
// that label from the catalog. The label must be non-empty.
func (s *Service) Delete(ctx context.Context, instanceID, label string) error {
	if strings.TrimSpace(label) == "" {
		return apperr.New(apperr.KindInvalid, "backup: delete requires a non-empty label")
	}

	inst, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	// Replicas have no backup stanza; backups live on the cluster primary.
	if inst.Role == instance.RoleReplica {
		return apperr.New(apperr.KindInvalid, "backup: replicas have no backups to delete; operate on the cluster primary")
	}

	cmd, err := pgbackrest.ExpireSet(inst.Stanza, confPath, label)
	if err != nil {
		return apperr.Wrap(apperr.KindInvalid, "backup: delete", err)
	}

	lock := s.lockFor(instanceID)
	lock.Lock()
	defer lock.Unlock()

	if err := s.execOK(ctx, inst.ContainerID, asPostgres(cmd)); err != nil {
		return err
	}
	if err := s.catalog.Delete(ctx, inst.ID, label); err != nil {
		return err
	}
	s.record(ctx, inst, "backup deleted ("+label+")", "", label)
	return nil
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

// Verify checks the integrity of the instance's repository (backup files and
// WAL) by running `pgbackrest verify`, and records a "backup verify" event. It
// does not modify the repo or catalog; a non-zero exit (corruption) is returned
// as an error.
func (s *Service) Verify(ctx context.Context, instanceID string) error {
	inst, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	// Replicas have no backup stanza; the repo lives on the cluster primary.
	if inst.Role == instance.RoleReplica {
		return apperr.New(apperr.KindInvalid, "backup: replicas have no repo to verify; operate on the cluster primary")
	}

	lock := s.lockFor(instanceID)
	lock.Lock()
	defer lock.Unlock()

	s.record(ctx, inst, "backup verify started", "", "")
	if err := s.execOK(ctx, inst.ContainerID, asPostgres(pgbackrest.Verify(inst.Stanza, confPath))); err != nil {
		return err
	}
	s.record(ctx, inst, "backup verify completed", "", "")
	return nil
}

// List returns the catalog for an instance.
func (s *Service) List(ctx context.Context, instanceID string) ([]Backup, error) {
	return s.catalog.List(ctx, instanceID)
}

// record writes a durable "backup" event. It is nil-safe (no recorder → no-op)
// and never fails the surrounding operation. backupType/label are added to the
// metadata only when non-empty.
func (s *Service) record(ctx context.Context, inst instance.Instance, message, backupType, label string) {
	if s.events == nil {
		return
	}
	meta := map[string]string{"instance": inst.Name}
	if backupType != "" {
		meta["backup_type"] = backupType
	}
	if label != "" {
		meta["label"] = label
	}
	_, _ = s.events.Record(ctx, events.NewEvent{
		InstanceID: inst.ID,
		Type:       "backup",
		Message:    message,
		Metadata:   meta,
	})
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
