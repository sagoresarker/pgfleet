package backup

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

const infoTwoBackups = `[{"name":"db","status":{"code":0,"message":"ok"},"backup":[
  {"label":"20260603-120000F","type":"full","timestamp":{"start":1717416000,"stop":1717416060},
   "info":{"size":1024,"delta":1024,"repository":{"size":512,"delta":512}},
   "archive":{"start":"a","stop":"b"},"reference":null},
  {"label":"20260603-130000F_I","type":"incr","timestamp":{"start":1717419900,"stop":1717419930},
   "info":{"size":2048,"delta":256,"repository":{"size":1024,"delta":128}},
   "archive":{"start":"c","stop":"d"},"reference":["20260603-120000F"]}
]}]`

type lookupFunc func(ctx context.Context, id string) (instance.Instance, error)

func (f lookupFunc) Get(ctx context.Context, id string) (instance.Instance, error) { return f(ctx, id) }

type fakeCatalog struct {
	mu       sync.Mutex
	upserted []string
	pruned   [][]string
}

func (c *fakeCatalog) Upsert(_ context.Context, _ string, b pgbackrest.BackupInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.upserted = append(c.upserted, b.Label)
	return nil
}
func (c *fakeCatalog) Prune(_ context.Context, _ string, keep []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruned = append(c.pruned, keep)
	return nil
}
func (c *fakeCatalog) List(context.Context, string) ([]Backup, error) { return nil, nil }

func runningInstance() instance.Instance {
	return instance.Instance{ID: "i1", Name: "db", Stanza: "db", ContainerID: "c1", Status: instance.StatusRunning}
}

func newService(rt docker.ContainerRuntime, cat catalog) *Service {
	lookup := lookupFunc(func(context.Context, string) (instance.Instance, error) { return runningInstance(), nil })
	return New(rt, lookup, cat)
}

func TestRunExecutesBackupAndSyncsCatalog(t *testing.T) {
	rt := docker.NewFake()
	var sawBackupType string
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		switch last(cmd) {
		case "backup":
			sawBackupType = typeFlag(cmd)
			return docker.ExecResult{ExitCode: 0}, nil
		case "info":
			return docker.ExecResult{ExitCode: 0, Stdout: infoTwoBackups}, nil
		}
		return docker.ExecResult{}, nil
	}
	cat := &fakeCatalog{}
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	// instance container id must match what the fake started:
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), cat)

	if err := svc.Run(context.Background(), "i1", "full"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sawBackupType != "full" {
		t.Errorf("backup type flag = %q, want full", sawBackupType)
	}
	if len(cat.upserted) != 2 {
		t.Errorf("catalog upserts = %d, want 2", len(cat.upserted))
	}
	if len(cat.pruned) != 1 || len(cat.pruned[0]) != 2 {
		t.Errorf("prune keep-set = %v, want 2 labels", cat.pruned)
	}
}

func TestRunRejectsInvalidType(t *testing.T) {
	svc := newService(docker.NewFake(), &fakeCatalog{})
	if err := svc.Run(context.Background(), "i1", "bogus"); err == nil {
		t.Error("invalid backup type should error")
	}
}

func TestRunFailsWhenBackupCommandFails(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "backup" {
			return docker.ExecResult{ExitCode: 1, Stderr: "disk full"}, nil
		}
		return docker.ExecResult{}, nil
	}
	cat := &fakeCatalog{}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), cat)

	err := svc.Run(context.Background(), "i1", "full")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("expected backup failure mentioning disk full, got %v", err)
	}
	if len(cat.upserted) != 0 {
		t.Error("catalog should not be synced after a failed backup")
	}
}

func TestSyncParsesAndUpserts(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "info" {
			return docker.ExecResult{Stdout: infoTwoBackups}, nil
		}
		return docker.ExecResult{}, nil
	}
	cat := &fakeCatalog{}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), cat)

	backups, err := svc.Sync(context.Background(), "i1")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(backups) != 2 {
		t.Errorf("parsed backups = %d, want 2", len(backups))
	}
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func typeFlag(cmd []string) string {
	for _, a := range cmd {
		if strings.HasPrefix(a, "--type=") {
			return strings.TrimPrefix(a, "--type=")
		}
	}
	return ""
}
