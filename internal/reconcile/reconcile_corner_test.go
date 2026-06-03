package reconcile

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// TestReconcileLeavesInFlightStatesAlone — provisioning/restoring/destroying
// instances are owned by another goroutine and must not be touched, even when
// their container is missing (which would otherwise look like an error).
func TestReconcileLeavesInFlightStatesAlone(t *testing.T) {
	for _, st := range []instance.Status{
		instance.StatusProvisioning, instance.StatusRestoring, instance.StatusDestroying,
	} {
		t.Run(string(st), func(t *testing.T) {
			rt := docker.NewFake()
			store := newStore(instance.Instance{ID: "i1", Name: "db", Status: st, ContainerID: "gone"})
			r := New(rt, store, nil)
			if err := r.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.status["i1"] != st {
				t.Errorf("status changed from %q to %q", st, store.status["i1"])
			}
		})
	}
}

// TestReconcileAdoptionRecordsHostPort — when adopting a rediscovered
// container, the parsed host port is recorded.
func TestReconcileAdoptionRecordsHostPort(t *testing.T) {
	rt := docker.NewFake()
	cid := startContainerFor(t, rt, "i1", true) // started => running, port assigned
	// DB lost track of the container id.
	store := newStore(instance.Instance{ID: "i1", Name: "db", Status: instance.StatusRunning, ContainerID: ""})
	r := New(rt, store, nil)

	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.runtime["i1"] != cid {
		t.Errorf("container not adopted: %q", store.runtime["i1"])
	}
	if store.ports["i1"] == 0 {
		t.Error("adopted host port should be non-zero")
	}
}

// TestHostPortParse — missing/malformed port strings parse to 0.
func TestHostPortParse(t *testing.T) {
	if got := hostPort(docker.ContainerState{Ports: map[string]string{}}); got != 0 {
		t.Errorf("missing port = %d, want 0", got)
	}
	if got := hostPort(docker.ContainerState{Ports: map[string]string{"5432/tcp": "abc"}}); got != 0 {
		t.Errorf("malformed port = %d, want 0", got)
	}
	if got := hostPort(docker.ContainerState{Ports: map[string]string{"5432/tcp": "32768"}}); got != 32768 {
		t.Errorf("valid port = %d, want 32768", got)
	}
}
