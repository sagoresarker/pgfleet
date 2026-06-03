package backup

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/events"
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
	deleted  []string
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
func (c *fakeCatalog) Delete(_ context.Context, _ string, label string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, label)
	return nil
}
func (c *fakeCatalog) List(context.Context, string) ([]Backup, error) { return nil, nil }

type fakeRecorder struct {
	mu     sync.Mutex
	events []events.NewEvent
}

func (r *fakeRecorder) Record(_ context.Context, ne events.NewEvent) (events.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ne)
	return events.Event{}, nil
}

func (r *fakeRecorder) snapshot() []events.NewEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]events.NewEvent, len(r.events))
	copy(out, r.events)
	return out
}

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

func TestRunWithAnnotationEmitsArg(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	var sawBackupCmd []string
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "backup" {
			sawBackupCmd = cmd
			return docker.ExecResult{ExitCode: 0}, nil
		}
		if last(cmd) == "info" {
			return docker.ExecResult{ExitCode: 0, Stdout: infoTwoBackups}, nil
		}
		return docker.ExecResult{}, nil
	}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), &fakeCatalog{})

	if err := svc.RunWith(context.Background(), "i1", "full", RunOpts{Annotation: "my nightly"}); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !containsArg(sawBackupCmd, "--annotation=name=my nightly") {
		t.Errorf("backup cmd missing annotation arg: %v", sawBackupCmd)
	}
}

func TestRunWithStandbyEmitsArg(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	var sawBackupCmd []string
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "backup" {
			sawBackupCmd = cmd
			return docker.ExecResult{ExitCode: 0}, nil
		}
		if last(cmd) == "info" {
			return docker.ExecResult{ExitCode: 0, Stdout: infoTwoBackups}, nil
		}
		return docker.ExecResult{}, nil
	}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), &fakeCatalog{})

	if err := svc.RunWith(context.Background(), "i1", "full", RunOpts{Standby: true}); err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if !containsArg(sawBackupCmd, "--backup-standby") {
		t.Errorf("backup cmd missing --backup-standby: %v", sawBackupCmd)
	}
}

func containsArg(cmd []string, want string) bool {
	for _, a := range cmd {
		if a == want {
			return true
		}
	}
	return false
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

func TestDeleteExpiresSetAndPrunesCatalog(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	var sawSet string
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "expire" {
			for _, a := range cmd {
				if strings.HasPrefix(a, "--set=") {
					sawSet = strings.TrimPrefix(a, "--set=")
				}
			}
			return docker.ExecResult{ExitCode: 0}, nil
		}
		return docker.ExecResult{}, nil
	}
	cat := &fakeCatalog{}
	rec := &fakeRecorder{}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), cat).WithEvents(rec)

	if err := svc.Delete(context.Background(), "i1", "20260603-120000F"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if sawSet != "20260603-120000F" {
		t.Errorf("expire --set = %q, want the deleted label", sawSet)
	}
	if len(cat.deleted) != 1 || cat.deleted[0] != "20260603-120000F" {
		t.Errorf("catalog deletes = %v, want [20260603-120000F]", cat.deleted)
	}
	// A delete event of type "backup" must be recorded.
	evs := rec.snapshot()
	var del *events.NewEvent
	for i := range evs {
		if evs[i].Type == "backup" && strings.Contains(evs[i].Message, "deleted") {
			del = &evs[i]
		}
	}
	if del == nil {
		t.Fatalf("no backup delete event recorded, got %+v", evs)
	}
	if del.InstanceID != "i1" || del.Metadata["label"] != "20260603-120000F" {
		t.Errorf("delete event = %+v, want instance i1 + label metadata", del)
	}
}

func TestDeleteRejectsEmptyLabel(t *testing.T) {
	cat := &fakeCatalog{}
	svc := newService(docker.NewFake(), cat)
	err := svc.Delete(context.Background(), "i1", "   ")
	if err == nil {
		t.Fatal("empty label should be rejected")
	}
	if len(cat.deleted) != 0 {
		t.Error("catalog must not be touched when label is invalid")
	}
}

func TestRunRecordsStartAndCompleteEvents(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "info" {
			return docker.ExecResult{ExitCode: 0, Stdout: infoTwoBackups}, nil
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	rec := &fakeRecorder{}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), &fakeCatalog{}).WithEvents(rec)

	if err := svc.Run(context.Background(), "i1", "full"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	evs := rec.snapshot()
	if len(evs) != 2 {
		t.Fatalf("recorded %d events, want 2 (start+complete): %+v", len(evs), evs)
	}
	if evs[0].Type != "backup" || !strings.Contains(evs[0].Message, "started") {
		t.Errorf("first event = %+v, want backup start", evs[0])
	}
	if evs[1].Type != "backup" || !strings.Contains(evs[1].Message, "completed") {
		t.Errorf("second event = %+v, want backup complete", evs[1])
	}
}

func TestRunWithoutRecorderIsNoOp(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "info" {
			return docker.ExecResult{ExitCode: 0, Stdout: infoTwoBackups}, nil
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), &fakeCatalog{}) // no WithEvents

	if err := svc.Run(context.Background(), "i1", "full"); err != nil {
		t.Fatalf("Run without recorder should still succeed: %v", err)
	}
}

func TestVerifyRunsVerifyAndRecordsEvent(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	var sawVerify bool
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "verify" {
			sawVerify = true
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	rec := &fakeRecorder{}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), &fakeCatalog{}).WithEvents(rec)

	if err := svc.Verify(context.Background(), "i1"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !sawVerify {
		t.Error("verify command was not run")
	}
	evs := rec.snapshot()
	var found bool
	for _, e := range evs {
		if e.Type == "backup" && strings.Contains(e.Message, "verify") {
			found = true
		}
	}
	if !found {
		t.Errorf("no backup verify event recorded, got %+v", evs)
	}
}

func TestVerifyFailsWhenCommandFails(t *testing.T) {
	rt := docker.NewFake()
	id, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{Name: "c1"})
	_ = rt.StartContainer(context.Background(), id)
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if last(cmd) == "verify" {
			return docker.ExecResult{ExitCode: 1, Stderr: "checksum mismatch"}, nil
		}
		return docker.ExecResult{}, nil
	}
	svc := New(rt, lookupFunc(func(context.Context, string) (instance.Instance, error) {
		in := runningInstance()
		in.ContainerID = id
		return in, nil
	}), &fakeCatalog{})

	err := svc.Verify(context.Background(), "i1")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected verify failure mentioning checksum mismatch, got %v", err)
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
