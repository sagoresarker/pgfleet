//go:build integration

package docker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuildAndRunManagedImage builds the postgres+pgBackRest image and verifies
// both binaries are present and runnable as the postgres user.
func TestBuildAndRunManagedImage(t *testing.T) {
	m := newMobyNoEnsure(t)
	ctx := context.Background()

	contextDir, err := filepath.Abs("../../docker/postgres-pgbackrest")
	if err != nil {
		t.Fatal(err)
	}

	tag := "pgfleet/postgres-pgbackrest:test-" + uniq()
	if err := m.BuildImage(ctx, contextDir, tag); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}

	id := createTestContainer(t, m, ContainerSpec{
		Name:  "pgfleet-test-img-" + uniq(),
		Image: tag,
		Cmd:   []string{"sleep", "300"},
		User:  "postgres",
	})
	if err := m.StartContainer(ctx, id); err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	pg, err := m.Exec(ctx, id, []string{"postgres", "--version"})
	if err != nil || pg.ExitCode != 0 || !strings.Contains(pg.Stdout, "16") {
		t.Errorf("postgres --version = %+v (err %v)", pg, err)
	}

	pgbr, err := m.Exec(ctx, id, []string{"pgbackrest", "version"})
	if err != nil || pgbr.ExitCode != 0 || !strings.Contains(pgbr.Stdout, "pgBackRest") {
		t.Errorf("pgbackrest version = %+v (err %v)", pgbr, err)
	}

	// pgBackRest must be runnable by the postgres user and own its dirs.
	owns, err := m.Exec(ctx, id, []string{"sh", "-c", "test -w /var/lib/pgbackrest && echo writable"})
	if err != nil || !strings.Contains(owns.Stdout, "writable") {
		t.Errorf("/var/lib/pgbackrest not writable by postgres: %+v (err %v)", owns, err)
	}
}

// newMobyNoEnsure builds a Moby without pre-pulling the alpine test image.
func newMobyNoEnsure(t *testing.T) *Moby {
	t.Helper()
	m, err := NewMoby()
	if err != nil {
		t.Fatalf("NewMoby: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
