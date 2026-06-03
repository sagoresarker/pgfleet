//go:build integration

package provision

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

// TestRestartPolicySurvivesContainerKill proves Phase 4.1: a managed instance
// with the unless-stopped restart policy comes back on its own after its
// container is killed (simulating a daemon/host crash), with NO control-plane
// action — the data volume is preserved and Postgres serves again.
func TestRestartPolicySurvivesContainerKill(t *testing.T) {
	ctx := context.Background()

	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatalf("NewMoby: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ensureManagedImage(t, rt)

	pool, _ := testsupport.MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)
	inst, err := repo.Create(ctx, instance.NewInstance{
		Name: "restart-" + shortID(), RepoType: instance.RepoLocal, Password: "test-password-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	p := New(rt, repo, Options{InstanceHost: "localhost", RestartPolicy: "unless-stopped"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })
	if err := p.Provision(ctx, inst.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	got, _ := repo.Get(ctx, inst.ID)
	cid := got.ContainerID

	// Confirm the policy is actually set on the running container.
	st, err := rt.Inspect(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	if st.RestartPolicy != "unless-stopped" {
		t.Fatalf("restart policy on container = %q, want unless-stopped", st.RestartPolicy)
	}

	// Simulate a crash: make the container's main process (the postmaster, PID
	// 1) exit on its own via an immediate shutdown. This is NOT a `docker
	// stop`/`docker kill` (the daemon flags those as a manual stop, which
	// unless-stopped deliberately does not restart), so the restart policy
	// fires — exactly the daemon-restart / process-crash recovery we want.
	_, _ = rt.Exec(ctx, cid, asPostgres([]string{"pg_ctl", "-D", pgDataPath, "stop", "-m", "immediate"}))

	// Wait for Docker's restart policy to bring the SAME container back.
	deadline := time.Now().Add(45 * time.Second)
	var lastStatus string
	for {
		st, err := rt.Inspect(ctx, cid)
		if err == nil {
			lastStatus = st.Status
			if st.Running {
				break // recovered, no control-plane action taken
			}
		}
		if time.Now().After(deadline) {
			ps, _ := exec.Command("docker", "inspect", "--format", "{{.State.Status}} restarts={{.RestartCount}} policy={{.HostConfig.RestartPolicy.Name}} exit={{.State.ExitCode}} err={{.State.Error}}", cid).CombinedOutput()
			t.Fatalf("container did not auto-restart within deadline (last status %q, err %v)\ndocker inspect: %s", lastStatus, err, ps)
		}
		time.Sleep(time.Second)
	}

	// And Postgres serves again on the preserved data volume. Check inside the
	// container (Docker may remap the dynamic host port on restart; the
	// reconciler refreshes the stored port on its next loop).
	var ready bool
	for range 30 {
		res, err := rt.Exec(ctx, cid, []string{"pg_isready", "-U", inst.Superuser})
		if err == nil && res.ExitCode == 0 {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("Postgres did not accept connections after auto-restart")
	}
}
