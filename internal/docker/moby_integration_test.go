//go:build integration

package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

const testImage = "alpine:3"

func newMoby(t *testing.T) *Moby {
	t.Helper()
	m, err := NewMoby()
	if err != nil {
		t.Fatalf("NewMoby: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.EnsureImage(context.Background(), testImage); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	return m
}

// uniqueName builds a label-tagged container name and registers cleanup.
func createTestContainer(t *testing.T, m *Moby, spec ContainerSpec) string {
	t.Helper()
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}
	spec.Labels[LabelManaged] = "true"
	id, err := m.CreateContainer(context.Background(), spec)
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	t.Cleanup(func() { _ = m.RemoveContainer(context.Background(), id, true) })
	return id
}

func TestMobyLifecycle(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	id := createTestContainer(t, m, ContainerSpec{
		Name:  "pgfleet-test-life-" + uniq(),
		Image: testImage,
		Cmd:   []string{"sleep", "300"},
	})

	if err := m.StartContainer(ctx, id); err != nil {
		t.Fatalf("StartContainer: %v", err)
	}
	st, err := m.Inspect(ctx, id)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !st.Running || st.Status != "running" {
		t.Errorf("running=%v status=%q, want true/running", st.Running, st.Status)
	}

	timeout := 1
	if err := m.StopContainer(ctx, id, &timeout); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if st, _ = m.Inspect(ctx, id); st.Running {
		t.Error("container should be stopped")
	}
}

func TestMobyExecCapturesStdoutAndExit(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	id := createTestContainer(t, m, ContainerSpec{Name: "pgfleet-test-exec-" + uniq(), Image: testImage, Cmd: []string{"sleep", "300"}})
	if err := m.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}

	res, err := m.Exec(ctx, id, []string{"echo", "hello-world"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "hello-world") {
		t.Errorf("exec result = %+v", res)
	}
}

func TestMobyExecCapturesStderrAndNonZeroExit(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	id := createTestContainer(t, m, ContainerSpec{Name: "pgfleet-test-execerr-" + uniq(), Image: testImage, Cmd: []string{"sleep", "300"}})
	if err := m.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}

	res, err := m.Exec(ctx, id, []string{"sh", "-c", "echo oops >&2; exit 7"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "oops") {
		t.Errorf("stderr = %q, want to contain oops", res.Stderr)
	}
}

func TestMobyLogs(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	id := createTestContainer(t, m, ContainerSpec{
		Name:  "pgfleet-test-logs-" + uniq(),
		Image: testImage,
		Cmd:   []string{"sh", "-c", "echo log-line-marker; sleep 5"},
	})
	if err := m.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	rc, err := m.Logs(ctx, id, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if !strings.Contains(string(data), "log-line-marker") {
		t.Errorf("logs missing marker: %q", string(data))
	}
}

func TestMobyListByLabel(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	instanceID := "inst-" + uniq()
	createTestContainer(t, m, ContainerSpec{
		Name:   "pgfleet-test-label-" + uniq(),
		Image:  testImage,
		Cmd:    []string{"sleep", "300"},
		Labels: map[string]string{LabelInstance: instanceID},
	})

	got, err := m.ListByLabel(ctx, map[string]string{LabelInstance: instanceID})
	if err != nil {
		t.Fatalf("ListByLabel: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListByLabel = %d, want 1", len(got))
	}
	if got[0].Labels[LabelInstance] != instanceID {
		t.Errorf("label not returned: %+v", got[0].Labels)
	}
}

func TestMobyVolumes(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	name := "pgfleet-test-vol-" + uniq()
	inst := "inst-" + uniq()

	if err := m.CreateVolume(ctx, name, map[string]string{LabelInstance: inst}); err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	t.Cleanup(func() { _ = m.RemoveVolume(ctx, name, true) })

	vols, err := m.ListVolumesByLabel(ctx, map[string]string{LabelInstance: inst})
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 || vols[0] != name {
		t.Errorf("ListVolumesByLabel = %v, want [%s]", vols, name)
	}

	if err := m.RemoveVolume(ctx, name, true); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}
	vols, _ = m.ListVolumesByLabel(ctx, map[string]string{LabelInstance: inst})
	if len(vols) != 0 {
		t.Errorf("volume not removed: %v", vols)
	}
}

func TestMobyNetworkAttach(t *testing.T) {
	m := newMoby(t)
	ctx := context.Background()
	netName := "pgfleet-test-net-" + uniq()

	netID, err := m.CreateNetwork(ctx, netName, map[string]string{LabelManaged: "true"})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	t.Cleanup(func() { _ = m.RemoveNetwork(ctx, netID) })

	id := createTestContainer(t, m, ContainerSpec{
		Name:     "pgfleet-test-netc-" + uniq(),
		Image:    testImage,
		Cmd:      []string{"sleep", "300"},
		Networks: []string{netName},
	})
	if err := m.StartContainer(ctx, id); err != nil {
		t.Fatalf("StartContainer on network: %v", err)
	}
	// Container on the network can be inspected without error.
	if _, err := m.Inspect(ctx, id); err != nil {
		t.Errorf("Inspect networked container: %v", err)
	}
}

func TestMobyInspectNotFound(t *testing.T) {
	m := newMoby(t)
	if _, err := m.Inspect(context.Background(), "does-not-exist-"+uniq()); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("kind = %v, want NotFound", apperr.Kind(err))
	}
}

var uniqCounter int

func uniq() string {
	uniqCounter++
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), uniqCounter)
}
