package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// TestContainerSpecAppendsUserParameters — validated user GUCs are appended as
// -c flags after the platform flags, and the platform preload library is set.
func TestContainerSpecAppendsUserParameters(t *testing.T) {
	p := New(docker.NewFake(), newStore(), Options{})
	inst := instance.Instance{
		Name: "tuned", Image: instance.DefaultImage, Stanza: "tuned", Superuser: "postgres",
		RepoType:   instance.RepoLocal,
		Parameters: map[string]string{"work_mem": "8MB"},
		Extensions: []string{"pg_trgm"},
	}
	spec := p.containerSpec(inst, "pw", nil)
	joined := strings.Join(spec.Cmd, " ")

	if !strings.Contains(joined, "work_mem=8MB") {
		t.Errorf("user parameter not appended: %v", spec.Cmd)
	}
	if !strings.Contains(joined, "shared_preload_libraries=pg_stat_statements") {
		t.Errorf("preload libraries not set: %v", spec.Cmd)
	}
	// The platform archive flags must still be present and unmodified.
	if !strings.Contains(joined, "archive_mode=on") {
		t.Errorf("platform flags lost: %v", spec.Cmd)
	}
}

// TestProvisionCreatesRequestedExtensions — provisioning runs CREATE EXTENSION
// for each requested extension (in addition to pg_stat_statements).
func TestProvisionCreatesRequestedExtensions(t *testing.T) {
	rt := docker.NewFake()
	var execs []string
	rt.ExecFunc = func(_ string, cmd []string) (docker.ExecResult, error) {
		execs = append(execs, strings.Join(cmd, " "))
		return docker.ExecResult{}, nil
	}
	store := newStore()
	store.inst.Extensions = []string{"pg_trgm", "citext"}

	p := New(rt, store, Options{})
	if err := p.Provision(context.Background(), "inst-1", nil); err != nil {
		t.Fatal(err)
	}

	all := strings.Join(execs, "\n")
	for _, ext := range []string{"pg_trgm", "citext", "pg_stat_statements"} {
		if !strings.Contains(all, "CREATE EXTENSION IF NOT EXISTS \""+ext+"\"") {
			t.Errorf("expected CREATE EXTENSION for %q in execs:\n%s", ext, all)
		}
	}
}
