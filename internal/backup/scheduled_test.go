package backup

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

type fakeLister struct{ items []instance.Instance }

func (f fakeLister) List(context.Context) ([]instance.Instance, error) { return f.items, nil }

func TestRunScheduledBacksUpOnlyRunningInstances(t *testing.T) {
	rt := docker.NewFake()
	cid, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c"})
	_ = rt.StartContainer(context.Background(), cid)

	var backups, expires int64
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		switch last(cmd) {
		case "backup":
			atomic.AddInt64(&backups, 1)
		case "expire":
			atomic.AddInt64(&expires, 1)
		case "info":
			return docker.ExecResult{Stdout: `[{"name":"db","status":{"code":0},"backup":[]}]`}, nil
		}
		return docker.ExecResult{}, nil
	}

	lookup := lookupFunc(func(_ context.Context, id string) (instance.Instance, error) {
		return instance.Instance{ID: id, Name: "db", Stanza: "db", ContainerID: cid, Status: instance.StatusRunning}, nil
	})
	svc := New(rt, lookup, &fakeCatalog{})

	lister := fakeLister{items: []instance.Instance{
		{ID: "i1", Status: instance.StatusRunning},
		{ID: "i2", Status: instance.StatusRunning},
		{ID: "i3", Status: instance.StatusStopped}, // skipped
	}}

	if err := svc.RunScheduled(context.Background(), lister, "full"); err != nil {
		t.Fatalf("RunScheduled: %v", err)
	}
	if got := atomic.LoadInt64(&backups); got != 2 {
		t.Errorf("backups run = %d, want 2 (running only)", got)
	}
	if got := atomic.LoadInt64(&expires); got != 2 {
		t.Errorf("expires run = %d, want 2", got)
	}
}
