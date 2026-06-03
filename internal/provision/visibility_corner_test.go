package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// failCreateRT wraps a Fake and fails a configurable number of the NEXT
// CreateContainer calls, to model a mid-flip create failure.
type failCreateRT struct {
	*docker.Fake
	failNext int // number of upcoming CreateContainer calls that should fail
}

func (r *failCreateRT) CreateContainer(ctx context.Context, spec docker.ContainerSpec) (string, error) {
	if r.failNext > 0 {
		r.failNext--
		return "", errors.New("create failed (simulated)")
	}
	return r.Fake.CreateContainer(ctx, spec)
}

// runningStandalone returns a store whose instance is a running standalone with
// an existing container, ready to flip visibility.
func runningStandalone() (*memStore, *docker.Fake, string) {
	rt := docker.NewFake()
	rt.ExecFunc = func(string, []string) (docker.ExecResult, error) { return docker.ExecResult{}, nil }
	cid, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{
		Name:   "pgfleet-pg-orders-db",
		Labels: map[string]string{docker.LabelInstance: "inst-1", docker.LabelRole: "postgres"},
	})
	_ = rt.StartContainer(context.Background(), cid)

	store := newStore()
	store.inst.Role = instance.RoleStandalone
	store.status = instance.StatusRunning
	store.container = cid
	store.dataVolume = "pgfleet-data-inst-1"
	store.inst.Public = false
	return store, rt, cid
}

// VIS-1/X-1: a transient create failure mid-flip (after the old container was
// removed) must self-heal — the instance is brought back up on its original
// binding and ends RUNNING with a live container, never wedged with none.
func TestSetVisibilityCreateFailureSelfHeals(t *testing.T) {
	store, fake, _ := runningStandalone()
	// The new-binding create fails; the recovery create (original binding)
	// succeeds.
	rt := &failCreateRT{Fake: fake, failNext: 1}
	p := New(rt, store, Options{})

	err := p.SetVisibility(context.Background(), "inst-1", true)
	if err == nil {
		t.Fatal("SetVisibility should report the create failure")
	}

	// Must NOT be wedged in StatusError (reconcile skips StatusError forever).
	if store.status == instance.StatusError {
		t.Errorf("instance left StatusError; reconcile skips StatusError so it is wedged forever")
	}
	// Recovered: running with a live container, and visibility rolled back.
	if store.status != instance.StatusRunning {
		t.Errorf("status = %q, want running after self-heal", store.status)
	}
	if _, ierr := rt.Inspect(context.Background(), store.container); ierr != nil {
		t.Errorf("instance has no live container after a mid-flip failure: %v", ierr)
	}
	if store.inst.Public {
		t.Error("visibility flag should be rolled back to the original on a failed flip")
	}
}

// VIS-1/X-1 (persistent failure): if the container can't be recreated at all,
// the instance must be left in a HEALABLE state (stopped), never wedged in
// StatusError, so it can be recovered once Docker is back.
func TestSetVisibilityPersistentCreateFailureIsHealable(t *testing.T) {
	store, fake, _ := runningStandalone()
	rt := &failCreateRT{Fake: fake, failNext: 99} // every create fails
	p := New(rt, store, Options{})

	err := p.SetVisibility(context.Background(), "inst-1", true)
	if err == nil {
		t.Fatal("SetVisibility should fail when no container can be created")
	}
	if store.status == instance.StatusError {
		t.Errorf("instance left StatusError; reconcile skips it forever (wedged)")
	}
	if store.status != instance.StatusStopped {
		t.Errorf("status = %q, want stopped (healable) after a persistent create failure", store.status)
	}
}

// VIS-2: visibility may only be flipped on a standalone instance. A replica or a
// clustered member must be rejected with KindInvalid (recreating it from a
// primary spec would corrupt it).
func TestSetVisibilityRejectsNonStandalone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    instance.Role
		cluster string
	}{
		{"replica", instance.RoleReplica, "cluster-1"},
		{"clustered primary", instance.RolePrimary, "cluster-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, rt, _ := runningStandalone()
			store.inst.Role = tc.role
			store.inst.ClusterID = tc.cluster
			p := New(rt, store, Options{})

			err := p.SetVisibility(context.Background(), "inst-1", true)
			if err == nil {
				t.Fatal("SetVisibility should reject a non-standalone instance")
			}
			if apperr.Kind(err) != apperr.KindInvalid {
				t.Errorf("kind = %v, want KindInvalid", apperr.Kind(err))
			}
		})
	}
}

// REG-2: a visibility flip must be refused unless the instance is in a stable
// state (running or stopped) — not while it is provisioning/restoring.
func TestSetVisibilityRefusedWhileUnstable(t *testing.T) {
	for _, st := range []instance.Status{
		instance.StatusProvisioning, instance.StatusRestoring, instance.StatusDestroying,
	} {
		t.Run(string(st), func(t *testing.T) {
			store, rt, _ := runningStandalone()
			store.status = st
			p := New(rt, store, Options{})

			err := p.SetVisibility(context.Background(), "inst-1", true)
			if err == nil {
				t.Fatalf("SetVisibility should be refused while %s", st)
			}
			if apperr.Kind(err) != apperr.KindInvalid {
				t.Errorf("kind = %v, want KindInvalid", apperr.Kind(err))
			}
		})
	}
}

// REG-2 (concurrency): concurrent visibility ops on the same instance must not
// both recreate a container. The provisioner serializes flips per instance.
func TestSetVisibilityConcurrentSafe(t *testing.T) {
	store, rt, _ := runningStandalone()
	p := New(rt, store, Options{})

	// Run two opposing flips concurrently. Whatever interleaving occurs, the
	// instance must end in a consistent state with exactly one live instance
	// container (role=postgres), never zero or a half-built duplicate.
	done := make(chan struct{}, 2)
	go func() { _ = p.SetVisibility(context.Background(), "inst-1", true); done <- struct{}{} }()
	go func() { _ = p.SetVisibility(context.Background(), "inst-1", false); done <- struct{}{} }()
	<-done
	<-done

	infos, _ := rt.ListByLabel(context.Background(), map[string]string{docker.LabelRole: "postgres"})
	if len(infos) != 1 {
		t.Errorf("expected exactly 1 instance container after concurrent flips, got %d", len(infos))
	}
}

// VIS-3: if writeConfig fails during a flip, the instance is running with broken
// archiving — the provisioner must surface an error and set an error status.
func TestSetVisibilityWriteConfigFailureSurfacesError(t *testing.T) {
	store, rt, _ := runningStandalone()
	// Fail only the writeConfig exec (its script chowns the repo path); pg_isready
	// and everything else succeed.
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "pgbackrest.conf") {
			return docker.ExecResult{ExitCode: 1, Stderr: "cannot write config"}, nil
		}
		return docker.ExecResult{ExitCode: 0}, nil
	}
	p := New(rt, store, Options{})

	err := p.SetVisibility(context.Background(), "inst-1", true)
	if err == nil {
		t.Fatal("SetVisibility should fail when writeConfig fails")
	}
	if store.status != instance.StatusError {
		t.Errorf("status = %q, want error after writeConfig failure", store.status)
	}
}

// Happy path still works: a running standalone flips and the new container binds
// public (0.0.0.0).
func TestSetVisibilityHappyPathRebinds(t *testing.T) {
	store, rt, oldCID := runningStandalone()
	p := New(rt, store, Options{BindAddress: "127.0.0.1"})

	if err := p.SetVisibility(context.Background(), "inst-1", true); err != nil {
		t.Fatalf("SetVisibility: %v", err)
	}
	if store.container == oldCID {
		t.Error("container was not recreated")
	}
	if !store.inst.Public {
		t.Error("instance not marked public")
	}
	if store.status != instance.StatusRunning {
		t.Errorf("status = %q, want running", store.status)
	}
}
