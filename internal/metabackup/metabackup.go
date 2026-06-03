// Package metabackup protects the control-plane meta database by dumping it to
// the object store (S3/MinIO) and restoring it. It shells out to pg_dump and
// pg_restore, which the control-plane image provides on PATH.
package metabackup

import (
	"bytes"
	"context"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/objectstore"
)

// stampLayout makes lexical order equal chronological order: a fixed-width,
// zero-padded UTC stamp (YYYYMMDDTHHMMSSZ).
const stampLayout = "20060102T150405Z"

// Service dumps and restores the control-plane meta database to/from the
// object store.
type Service struct {
	store  objectstore.Config
	now    func() time.Time
	prefix string
}

// New builds a meta-backup Service writing under the default "meta-backups/"
// prefix.
func New(store objectstore.Config) *Service {
	return &Service{
		store:  store,
		now:    time.Now,
		prefix: "meta-backups/",
	}
}

// stampKey returns the full object key for a dump taken at t. Keys sort
// lexicographically into chronological order.
func (s *Service) stampKey(t time.Time) string {
	return s.prefix + "pgfleet-meta-" + t.UTC().Format(stampLayout) + ".dump"
}

// Backup runs pg_dump against dsn, captures the custom-format dump, and uploads
// it to the object store. It returns the object key.
func (s *Service) Backup(ctx context.Context, dsn string) (string, error) {
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--dbname="+dsn)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", apperr.Wrap(apperr.KindInternal,
			"metabackup: pg_dump failed: "+strings.TrimSpace(stderr.String()), err)
	}

	key := s.stampKey(s.now())
	if err := objectstore.PutObject(ctx, s.store, key, stdout.Bytes()); err != nil {
		return "", err
	}
	return key, nil
}

// List returns the dump keys in the object store, sorted chronologically
// (oldest first).
func (s *Service) List(ctx context.Context) ([]string, error) {
	keys, err := objectstore.ListObjects(ctx, s.store, s.prefix)
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

// Prune keeps the newest keep dumps and deletes the rest. A non-positive keep
// deletes nothing.
func (s *Service) Prune(ctx context.Context, keep int) error {
	if keep <= 0 {
		return nil
	}
	keys, err := s.List(ctx)
	if err != nil {
		return err
	}
	if len(keys) <= keep {
		return nil
	}
	// keys are oldest-first; delete all but the newest keep.
	for _, key := range keys[:len(keys)-keep] {
		if err := objectstore.DeleteObject(ctx, s.store, key); err != nil {
			return err
		}
	}
	return nil
}

// Restore fetches the dump at key and pipes it into pg_restore against dsn.
func (s *Service) Restore(ctx context.Context, dsn, key string) error {
	data, err := objectstore.GetObject(ctx, s.store, key)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "pg_restore",
		"--clean", "--if-exists", "--no-owner", "--dbname="+dsn)
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return apperr.Wrap(apperr.KindInternal,
			"metabackup: pg_restore failed: "+strings.TrimSpace(stderr.String()), err)
	}
	return nil
}
