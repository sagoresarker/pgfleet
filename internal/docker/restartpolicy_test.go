package docker

import (
	"context"
	"testing"
)

// TestFakeRecordsRestartPolicy — the runtime must carry a container's restart
// policy through create and report it back via Inspect, so provisioning can set
// it and tests can assert it.
func TestFakeRecordsRestartPolicy(t *testing.T) {
	f := NewFake()
	id, err := f.CreateContainer(context.Background(), ContainerSpec{
		Name:          "c",
		RestartPolicy: "unless-stopped",
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if st.RestartPolicy != "unless-stopped" {
		t.Errorf("RestartPolicy = %q, want unless-stopped", st.RestartPolicy)
	}
}
