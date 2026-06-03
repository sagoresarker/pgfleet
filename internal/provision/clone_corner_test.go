package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// cloneSource registers a running source container in the fake and returns a
// matching source instance whose ContainerID points at it (so the pre-flight
// `pgbackrest info` exec has a live container to run against).
func cloneSource(rt *docker.Fake) instance.Instance {
	cid, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{
		Name:   "pgfleet-pg-orders-src",
		Labels: map[string]string{docker.LabelInstance: "src-1", docker.LabelRole: "postgres"},
	})
	_ = rt.StartContainer(context.Background(), cid)
	return instance.Instance{
		ID: "src-1", Name: "orders-src", Image: instance.DefaultImage,
		RepoType: instance.RepoLocal, Stanza: "orders-src", Superuser: "postgres",
		ContainerID: cid, Status: instance.StatusRunning,
	}
}

// infoWithBackup is a minimal `pgbackrest info --output=json` payload with one
// full backup present.
const infoWithBackup = `[{"name":"orders-src","status":{"code":0,"message":"ok"},` +
	`"backup":[{"label":"20240101-000000F","type":"full"}]}]`

// infoNoBackup reports a stanza that exists but has no backups yet.
const infoNoBackup = `[{"name":"orders-src","status":{"code":0,"message":"ok"},"backup":[]}]`

// newCloneStore returns a store whose Get yields the CLONE instance (a fresh
// local-repo standalone).
func newCloneStore() *memStore {
	s := newStore()
	s.inst = instance.Instance{
		ID: "clone-1", Name: "orders-clone", Image: instance.DefaultImage,
		RepoType: instance.RepoLocal, Stanza: "orders-clone", Superuser: "postgres",
	}
	s.status = instance.StatusProvisioning
	return s
}

// isInfoCmd reports whether cmd is a `pgbackrest ... info` invocation.
func isInfoCmd(cmd []string) bool {
	return contains(cmd, "info")
}

// CL-1: during a clone the SOURCE's pgBackRest repo volume must be mounted
// READ-ONLY into the one-shot restore container, so a restore can never write
// into / corrupt the source's live backup repo.
func TestCloneMountsSourceRepoReadOnly(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if isInfoCmd(cmd) {
			return docker.ExecResult{ExitCode: 0, Stdout: infoWithBackup}, nil
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	// Capture the restore container's spec at creation time (it is removed once
	// it exits, so we cannot inspect it afterwards).
	var restoreSpec docker.ContainerSpec
	var sawRestore bool
	rt.OnCreate = func(spec docker.ContainerSpec) {
		if spec.Labels[docker.LabelRole] == "restore" {
			restoreSpec = spec
			sawRestore = true
		}
	}
	// The one-shot restore container must exit cleanly so clone proceeds.
	rt.OnStart = func(f *docker.Fake, _ string) {
		infos, _ := f.ListByLabel(context.Background(), map[string]string{docker.LabelRole: "restore"})
		for _, in := range infos {
			f.MarkExited(in.ID, 0)
		}
	}

	store := newCloneStore()
	p := New(rt, store, Options{})

	// We expect the clone to run far enough to create the restore container.
	_ = p.Clone(context.Background(), "clone-1", cloneSource(rt), nil)

	if !sawRestore {
		t.Fatal("restore container was never created")
	}
	var sawSourceRepo bool
	for _, m := range restoreSpec.Mounts {
		if m.Path == repoPath {
			sawSourceRepo = true
			if !m.ReadOnly {
				t.Errorf("source repo volume %q mounted read-write; must be read-only", m.Volume)
			}
		}
	}
	if !sawSourceRepo {
		t.Fatal("restore container had no source repo mount")
	}
}

// CL-2: cloning from a source whose stanza has NO backup yet must fail with a
// clear KindInvalid pre-flight error, not an opaque pgBackRest log error.
func TestCloneRejectsSourceWithNoBackup(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		if isInfoCmd(cmd) {
			return docker.ExecResult{ExitCode: 0, Stdout: infoNoBackup}, nil
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	store := newCloneStore()
	p := New(rt, store, Options{})

	err := p.Clone(context.Background(), "clone-1", cloneSource(rt), nil)
	if err == nil {
		t.Fatal("Clone should fail when the source has no backup")
	}
	if apperr.Kind(err) != apperr.KindInvalid {
		t.Errorf("error kind = %v, want KindInvalid", apperr.Kind(err))
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no backup") {
		t.Errorf("error = %q, want it to mention no backup", err.Error())
	}
	// No restore container should have been created.
	if _, ok := rt.SpecByRole("restore"); ok {
		t.Error("restore container was created despite the empty-source pre-flight failure")
	}
}
