// Package metabackup protects the control-plane meta database by dumping it to
// the object store (S3/MinIO) and restoring it. It shells out to pg_dump and
// pg_restore, which the control-plane image provides on PATH.
package metabackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
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

// stampKey returns the full object key for a dump taken at t. The full,
// fixed-width timestamp comes first so keys sort lexicographically into
// chronological order; a short random suffix is appended AFTER the stamp so two
// backups taken within the same second (1s stamp resolution) still produce
// distinct keys and do not overwrite each other (MB-1). Because the suffix
// follows the complete stamp, keys from different seconds still sort by time
// regardless of their suffixes.
func (s *Service) stampKey(t time.Time) string {
	return s.prefix + "pgfleet-meta-" + t.UTC().Format(stampLayout) + "-" + uniqueSuffix() + ".dump"
}

// uniqueSuffix returns a short, lowercase-hex random token. It uses crypto/rand
// so concurrent or rapid same-second backups get collision-free keys. On the
// astronomically unlikely event that the entropy source fails, it falls back to
// the nanosecond component of the wall clock, which still distinguishes
// same-second calls in practice.
func uniqueSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ToLower(time.Now().UTC().Format("000000000"))
	}
	return hex.EncodeToString(b[:])
}

// pgDumpVersionRe captures the major version from `pg_dump --version` output,
// e.g. "pg_dump (PostgreSQL) 16.2" -> "16", "pg_dump (PostgreSQL) 17rc1" -> "17".
var pgDumpVersionRe = regexp.MustCompile(`\(PostgreSQL\)\s+(\d+)`)

// parsePgDumpMajor extracts the major version from `pg_dump --version` output.
// It returns ok=false when the output does not contain a recognizable version.
func parsePgDumpMajor(out string) (int, bool) {
	m := pgDumpVersionRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// serverMajorFromVersionNum converts a Postgres server_version_num (e.g. 160002
// for 16.2, or 90624 for 9.6.24) into its major version. Since PG 10 the major
// version is num/10000; the same formula yields 9 for the 9.x line, which is the
// granularity pg_dump version-compatibility cares about.
func serverMajorFromVersionNum(num int) int {
	return num / 10000
}

// splitDSNPassword strips the password from a URL-form DSN, returning the
// password-free DSN and the password separately, so the password is passed via
// PGPASSWORD env rather than the process argv (visible in ps/proc). A DSN
// without a parseable password is returned unchanged with an empty password.
func splitDSNPassword(dsn string) (clean, password string) {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn, ""
	}
	pw, has := u.User.Password()
	if !has {
		return dsn, ""
	}
	u.User = url.User(u.User.Username())
	return u.String(), pw
}

// withPGPassword returns the process env plus PGPASSWORD (when non-empty).
func withPGPassword(pw string) []string {
	if pw == "" {
		return os.Environ()
	}
	return append(os.Environ(), "PGPASSWORD="+pw)
}

// redactPW scrubs the password from text (e.g. libpq stderr that echoes the DSN).
func redactPW(s, pw string) string {
	if pw == "" {
		return s
	}
	return strings.ReplaceAll(s, pw, "****")
}

// Backup runs pg_dump against dsn, captures the custom-format dump, and uploads
// it to the object store. It returns the object key.
func (s *Service) Backup(ctx context.Context, dsn string) (string, error) {
	clean, pw := splitDSNPassword(dsn)
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--dbname="+clean)
	cmd.Env = withPGPassword(pw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", apperr.Wrap(apperr.KindInternal,
			"metabackup: pg_dump failed: "+redactPW(strings.TrimSpace(stderr.String()), pw), err)
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

	clean, pw := splitDSNPassword(dsn)
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--clean", "--if-exists", "--no-owner", "--dbname="+clean)
	cmd.Env = withPGPassword(pw)
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return apperr.Wrap(apperr.KindInternal,
			"metabackup: pg_restore failed: "+redactPW(strings.TrimSpace(stderr.String()), pw), err)
	}
	return nil
}
