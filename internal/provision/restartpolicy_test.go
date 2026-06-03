package provision

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
)

// TestProvisionSetsRestartPolicy — a provisioned instance container must carry
// the configured restart policy so it survives a daemon/host restart without
// the control plane.
func TestProvisionSetsRestartPolicy(t *testing.T) {
	rt := docker.NewFake()
	rt.ExecFunc = func(string, []string) (docker.ExecResult, error) { return docker.ExecResult{}, nil }
	store := newStore()
	p := New(rt, store, Options{RestartPolicy: "unless-stopped"})

	if err := p.Provision(context.Background(), "inst-1", nil); err != nil {
		t.Fatal(err)
	}
	st, err := rt.Inspect(context.Background(), store.container)
	if err != nil {
		t.Fatal(err)
	}
	if st.RestartPolicy != "unless-stopped" {
		t.Errorf("RestartPolicy = %q, want unless-stopped", st.RestartPolicy)
	}
}
