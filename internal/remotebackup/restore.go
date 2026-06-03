package remotebackup

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// RestoreInto loads a previously-captured custom-format dump (identified by its
// catalog id) into the target database addressed by targetDSN. The target is a
// freshly provisioned PgFleet instance (single or a cluster primary); its DSN
// carries the new superuser credentials.
//
// pg_restore runs with --no-owner --no-privileges so the source's roles do not
// have to exist on the target, and the password is supplied via PGPASSWORD (the
// targetDSN's password is parsed out and moved to the environment so it never
// hits argv). On failure the error text is scrubbed of the target password.
func (s *Service) RestoreInto(ctx context.Context, dumpID, targetDSN string) error {
	_, data, err := s.Fetch(ctx, dumpID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()

	return s.runRestoreFn()(ctx, data, targetDSN)
}

// runRestore is overridable in tests; it defaults to realRestore.
func (s *Service) runRestoreFn() func(context.Context, []byte, string) error {
	if s.runRestore != nil {
		return s.runRestore
	}
	return realRestore
}

// realRestore pipes the dump bytes into pg_restore against the target DSN.
func realRestore(ctx context.Context, data []byte, targetDSN string) error {
	cfg, err := pgconn.ParseConfig(targetDSN)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "remotebackup: parse target dsn", err)
	}
	args := buildRestoreArgs(cfg.User, cfg.Database, cfg.Host, int(cfg.Port))

	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Env = withPGPassword(cfg.Password)
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := Redact(strings.TrimSpace(stderr.String()), cfg.Password)
		return apperr.New(apperr.KindInternal, "remotebackup: pg_restore failed: "+msg)
	}
	return nil
}
