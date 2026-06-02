//go:build integration

package testsupport

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/provision"
	"github.com/sagoresarker/pgfleet/internal/secrets"
)

var nameCounter int

// ProvisionLocalInstance builds the managed image, provisions a local-repo
// Postgres instance, and returns handles plus the provisioner. The instance is
// destroyed on cleanup. Must be called from a package other than provision
// (it imports provision).
func ProvisionLocalInstance(t *testing.T) (instance.Instance, *provision.Provisioner, *instance.Repository, *docker.Moby) {
	t.Helper()
	ctx := context.Background()

	rt, err := docker.NewMoby()
	if err != nil {
		t.Fatalf("NewMoby: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	dir, err := filepath.Abs("../../docker/postgres-pgbackrest")
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.BuildImage(ctx, dir, instance.DefaultImage); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}

	pool, _ := MigratedPool(t)
	cipher, _ := secrets.New(make([]byte, 32))
	repo := instance.NewRepository(pool, cipher)

	nameCounter++
	name := "ts-" + string(rune('a'+nameCounter%26)) + string(rune('a'+(nameCounter/26)%26))
	inst, err := repo.Create(ctx, instance.NewInstance{
		Name: name, RepoType: instance.RepoLocal, Password: "test-password-1",
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	p := provision.New(rt, repo, provision.Options{InstanceHost: "localhost"})
	t.Cleanup(func() { _ = p.Destroy(ctx, inst.ID, false) })
	if err := p.Provision(ctx, inst.ID, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	got, _ := repo.Get(ctx, inst.ID)
	return got, p, repo, rt
}
