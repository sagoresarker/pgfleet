package provision

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// TestRestoreDeltaReachesRestoreContainer — passing Delta=true threads --delta
// into the restore command run inside the one-shot restore container.
func TestRestoreDeltaReachesRestoreContainer(t *testing.T) {
	rt := docker.NewFake()
	var restoreScript string
	rt.OnStart = func(f *docker.Fake, _ string) {
		if spec, ok := f.SpecByRole("restore"); ok && restoreScript == "" {
			restoreScript = strings.Join(spec.Cmd, " ")
		}
		infos, _ := f.ListByLabel(context.Background(), map[string]string{docker.LabelRole: "restore"})
		for _, in := range infos {
			f.MarkExited(in.ID, 0)
		}
	}
	origID, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{
		Name:   "pgfleet-orders-db",
		Labels: map[string]string{docker.LabelInstance: "inst-1"},
	})
	_ = rt.StartContainer(context.Background(), origID)

	store := newStore()
	store.container = origID
	store.dataVolume = "pgfleet-data-inst-1"

	p := New(rt, store, Options{})
	_ = p.Restore(context.Background(), "inst-1", RestoreOptions{Delta: true}, nil)

	if restoreScript == "" {
		t.Fatal("no restore container was created")
	}
	if !strings.Contains(restoreScript, "--delta") {
		t.Errorf("restore script missing --delta:\n%s", restoreScript)
	}
}

// TestRestorePasswordFailureRestartsOriginal — if fetching the superuser
// password fails AFTER the staging restore container has run (the instance is
// already stopped), restore must roll back by restarting the ORIGINAL
// instance, not leave it stopped indefinitely.
func TestRestorePasswordFailureRestartsOriginal(t *testing.T) {
	rt := docker.NewFake()
	// One-shot restore container exits cleanly so the restore proceeds to the
	// password fetch.
	rt.OnStart = func(f *docker.Fake, _ string) {
		infos, _ := f.ListByLabel(context.Background(), map[string]string{docker.LabelRole: "restore"})
		for _, in := range infos {
			f.MarkExited(in.ID, 0)
		}
	}

	// Create the original instance container so there is something to restart.
	origID, _ := rt.CreateContainer(context.Background(), docker.ContainerSpec{
		Name:   "pgfleet-orders-db",
		Labels: map[string]string{docker.LabelInstance: "inst-1"},
	})
	_ = rt.StartContainer(context.Background(), origID)

	store := newStore()
	store.container = origID
	store.dataVolume = "pgfleet-data-inst-1"
	store.passwordErr = errors.New("secrets store unavailable")

	p := New(rt, store, Options{})
	err := p.Restore(context.Background(), "inst-1", RestoreOptions{}, nil)
	if err == nil {
		t.Fatal("Restore should fail when the password fetch fails")
	}

	// The original container must be running again (rolled back), not left
	// stopped.
	state, ierr := rt.Inspect(context.Background(), origID)
	if ierr != nil {
		t.Fatalf("original container was removed: %v", ierr)
	}
	if !state.Running {
		t.Error("original instance was left stopped after a failed restore (no rollback)")
	}
	_ = instance.RoleStandalone
}
