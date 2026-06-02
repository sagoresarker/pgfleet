package docker

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestFakeContainerLifecycle(t *testing.T) {
	rt := NewFake()
	ctx := context.Background()

	id, err := rt.CreateContainer(ctx, ContainerSpec{
		Name:   "c1",
		Image:  "postgres:16",
		Labels: map[string]string{"pgfleet.instance": "abc"},
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}

	st, err := rt.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if st.Running || st.Status != "created" {
		t.Errorf("after create: running=%v status=%q, want false/created", st.Running, st.Status)
	}

	if err := rt.StartContainer(ctx, id); err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	if st, _ = rt.Inspect(ctx, id); !st.Running || st.Status != "running" {
		t.Errorf("after start: running=%v status=%q, want true/running", st.Running, st.Status)
	}

	if err := rt.StopContainer(ctx, id, nil); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if st, _ = rt.Inspect(ctx, id); st.Running || st.Status != "exited" {
		t.Errorf("after stop: running=%v status=%q, want false/exited", st.Running, st.Status)
	}

	if err := rt.RemoveContainer(ctx, id, false); err != nil {
		t.Fatalf("RemoveContainer: %v", err)
	}
	if _, err := rt.Inspect(ctx, id); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("Inspect after remove kind = %v, want NotFound", apperr.Kind(err))
	}
}

func TestFakeStartUnknownContainerErrors(t *testing.T) {
	rt := NewFake()
	if err := rt.StartContainer(context.Background(), "ghost"); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("kind = %v, want NotFound", apperr.Kind(err))
	}
}

func TestFakeRemoveRunningRequiresForce(t *testing.T) {
	rt := NewFake()
	ctx := context.Background()
	id, _ := rt.CreateContainer(ctx, ContainerSpec{Name: "c", Image: "img"})
	_ = rt.StartContainer(ctx, id)

	if err := rt.RemoveContainer(ctx, id, false); err == nil {
		t.Error("removing a running container without force should error")
	}
	if err := rt.RemoveContainer(ctx, id, true); err != nil {
		t.Errorf("force remove should succeed: %v", err)
	}
}

func TestFakeExecScripted(t *testing.T) {
	rt := NewFake()
	ctx := context.Background()
	id, _ := rt.CreateContainer(ctx, ContainerSpec{Name: "c", Image: "img"})
	_ = rt.StartContainer(ctx, id)

	rt.ExecFunc = func(_ string, cmd []string) (ExecResult, error) {
		if len(cmd) > 0 && cmd[0] == "pgbackrest" {
			return ExecResult{ExitCode: 0, Stdout: "[]"}, nil
		}
		return ExecResult{ExitCode: 127, Stderr: "not found"}, nil
	}

	res, err := rt.Exec(ctx, id, []string{"pgbackrest", "info"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 || res.Stdout != "[]" {
		t.Errorf("exec result = %+v", res)
	}
}

func TestFakeExecRequiresRunningContainer(t *testing.T) {
	rt := NewFake()
	ctx := context.Background()
	id, _ := rt.CreateContainer(ctx, ContainerSpec{Name: "c", Image: "img"})

	if _, err := rt.Exec(ctx, id, []string{"echo", "hi"}); err == nil {
		t.Error("exec on a non-running container should error")
	}
}

func TestFakeListByLabel(t *testing.T) {
	rt := NewFake()
	ctx := context.Background()
	_, _ = rt.CreateContainer(ctx, ContainerSpec{Name: "a", Image: "img", Labels: map[string]string{"pgfleet.managed": "true", "pgfleet.instance": "1"}})
	_, _ = rt.CreateContainer(ctx, ContainerSpec{Name: "b", Image: "img", Labels: map[string]string{"pgfleet.managed": "true", "pgfleet.instance": "2"}})
	_, _ = rt.CreateContainer(ctx, ContainerSpec{Name: "other", Image: "img", Labels: map[string]string{"unrelated": "x"}})

	got, err := rt.ListByLabel(ctx, map[string]string{"pgfleet.managed": "true"})
	if err != nil {
		t.Fatalf("ListByLabel: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByLabel returned %d, want 2", len(got))
	}

	got, _ = rt.ListByLabel(ctx, map[string]string{"pgfleet.instance": "1"})
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("filter by instance=1 = %+v", got)
	}
}

func TestFakeVolumes(t *testing.T) {
	rt := NewFake()
	ctx := context.Background()

	if err := rt.CreateVolume(ctx, "vol1", map[string]string{"pgfleet.instance": "1"}); err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	vols, err := rt.ListVolumesByLabel(ctx, map[string]string{"pgfleet.instance": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 || vols[0] != "vol1" {
		t.Errorf("ListVolumesByLabel = %v", vols)
	}
	if err := rt.RemoveVolume(ctx, "vol1", false); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}
	vols, _ = rt.ListVolumesByLabel(ctx, map[string]string{"pgfleet.instance": "1"})
	if len(vols) != 0 {
		t.Errorf("volume not removed: %v", vols)
	}
}

// Compile-time assertion that the fake satisfies the interface.
var _ ContainerRuntime = (*Fake)(nil)
