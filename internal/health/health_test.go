package health

import (
	"context"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

type lookupFunc func(ctx context.Context, id string) (instance.Instance, error)

func (f lookupFunc) Get(ctx context.Context, id string) (instance.Instance, error) { return f(ctx, id) }

type listFunc func(ctx context.Context, id string) ([]backup.Backup, error)

func (f listFunc) List(ctx context.Context, id string) ([]backup.Backup, error) { return f(ctx, id) }

func fixedNow() time.Time { return time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC) }

// runningContainer creates and starts a fake container, returning its id.
func runningContainer(rt *docker.Fake) string {
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c"})
	_ = rt.StartContainer(context.Background(), id)
	return id
}

func newChecker(rt *docker.Fake, backups []backup.Backup, th Thresholds) *Checker {
	cid := runningContainer(rt)
	c := NewChecker(rt,
		lookupFunc(func(_ context.Context, id string) (instance.Instance, error) {
			return instance.Instance{ID: id, Stanza: "db", ContainerID: cid}, nil
		}),
		listFunc(func(context.Context, string) ([]backup.Backup, error) { return backups, nil }),
		th,
	)
	c.now = fixedNow
	return c
}

// execScript drives the fake runtime: check exit code, du output.
func execScript(checkExit int, walBytes string) func(string, []string) (docker.ExecResult, error) {
	return func(_ string, cmd []string) (docker.ExecResult, error) {
		switch cmd[len(cmd)-1] {
		case "check":
			return docker.ExecResult{ExitCode: checkExit}, nil
		}
		if cmd[0] == "du" {
			return docker.ExecResult{ExitCode: 0, Stdout: walBytes + "\t/var/lib/postgresql/data/pg_wal"}, nil
		}
		return docker.ExecResult{}, nil
	}
}

func recentBackup() backup.Backup {
	return backup.Backup{Label: "L1", Type: "full", StoppedAt: fixedNow().Add(-1 * time.Hour)}
}

func TestCheckHealthyInstance(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = execScript(0, "1024")
	c := newChecker(rt, []backup.Backup{recentBackup()}, DefaultThresholds())

	r, err := c.Check(context.Background(), "i1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Healthy() {
		t.Errorf("expected healthy, got issues: %v", r.Issues)
	}
	if !r.ArchivingOK || !r.HasBackup || r.WALBytes != 1024 {
		t.Errorf("report = %+v", r)
	}
}

func TestCheckArchivingFailing(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = execScript(1, "1024") // check fails
	c := newChecker(rt, []backup.Backup{recentBackup()}, DefaultThresholds())

	r, _ := c.Check(context.Background(), "i1")
	if r.ArchivingOK {
		t.Error("ArchivingOK should be false")
	}
	if r.Healthy() {
		t.Error("expected an archiving issue")
	}
}

func TestCheckNoBackups(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = execScript(0, "1024")
	c := newChecker(rt, nil, DefaultThresholds())

	r, _ := c.Check(context.Background(), "i1")
	if r.HasBackup {
		t.Error("HasBackup should be false")
	}
	if r.Healthy() {
		t.Error("expected a no-backups issue")
	}
}

func TestCheckStaleBackup(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = execScript(0, "1024")
	old := backup.Backup{Label: "old", StoppedAt: fixedNow().Add(-48 * time.Hour)}
	c := newChecker(rt, []backup.Backup{old}, DefaultThresholds())

	r, _ := c.Check(context.Background(), "i1")
	if r.Healthy() {
		t.Errorf("expected stale-backup issue, got %v", r.Issues)
	}
}

func TestCheckWALPressure(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = execScript(0, "999999999999") // huge pg_wal
	c := newChecker(rt, []backup.Backup{recentBackup()}, Thresholds{MaxBackupAge: 25 * time.Hour, MaxWALBytes: 1024})

	r, _ := c.Check(context.Background(), "i1")
	if r.Healthy() {
		t.Errorf("expected pg_wal pressure issue, got %v", r.Issues)
	}
}
