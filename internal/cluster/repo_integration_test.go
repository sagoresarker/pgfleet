//go:build integration

package cluster

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func TestClusterCRUDAndMembership(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	repo := NewRepository(pool)

	c, err := repo.Create(ctx, NewCluster{Name: "orders"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.Status != StatusProvisioning || c.PrimaryInstanceID != "" {
		t.Errorf("new cluster = %+v", c)
	}

	// Duplicate name conflicts.
	if _, err := repo.Create(ctx, NewCluster{Name: "orders"}); apperr.Kind(err) != apperr.KindConflict {
		t.Errorf("duplicate name kind = %v, want Conflict", apperr.Kind(err))
	}

	// Provision a primary + replica belonging to the cluster.
	cipher, _ := secrets.New(make([]byte, 32))
	instRepo := instance.NewRepository(pool, cipher)
	primary, _ := instRepo.Create(ctx, instance.NewInstance{
		Name: "orders-p", RepoType: instance.RepoLocal, Password: "a-good-password",
		ClusterID: c.ID, Role: instance.RolePrimary,
	})
	_, _ = instRepo.Create(ctx, instance.NewInstance{
		Name: "orders-r1", RepoType: instance.RepoLocal, Password: "a-good-password",
		ClusterID: c.ID, Role: instance.RoleReplica,
	})

	if err := repo.SetPrimary(ctx, c.ID, primary.ID); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}
	if err := repo.SetRouter(ctx, c.ID, "router-container", 6432); err != nil {
		t.Fatalf("SetRouter: %v", err)
	}
	if err := repo.SetStatus(ctx, c.ID, StatusRunning, ""); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, _ := repo.Get(ctx, c.ID)
	if got.PrimaryInstanceID != primary.ID || got.RouterPort != 6432 || got.Status != StatusRunning {
		t.Errorf("cluster after updates = %+v", got)
	}

	// Membership query returns primary first.
	members, err := instRepo.ListByCluster(ctx, c.ID)
	if err != nil {
		t.Fatalf("ListByCluster: %v", err)
	}
	if len(members) != 2 || members[0].Role != instance.RolePrimary {
		t.Errorf("members = %+v", members)
	}

	// Deleting the cluster cascades to its instances.
	if err := repo.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := instRepo.Get(ctx, primary.ID); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("primary should be cascade-deleted, kind = %v", apperr.Kind(err))
	}
}
