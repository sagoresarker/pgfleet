//go:build integration

package user

import (
	"context"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func newRepo(t *testing.T) *Repository {
	pool, _ := testsupport.MigratedPool(t)
	return NewRepository(pool)
}

func TestCreateThenGetByEmail(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, NewUser{
		Email: "Admin@Example.com", PasswordHash: "hash1", Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Errorf("Create did not populate db-generated fields: %+v", created)
	}
	if created.Email != "admin@example.com" {
		t.Errorf("email not normalized on store: %q", created.Email)
	}

	// Case-insensitive lookup.
	got, err := repo.GetByEmail(ctx, "admin@EXAMPLE.com")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != created.ID || got.Role != auth.RoleAdmin {
		t.Errorf("GetByEmail mismatch: %+v", got)
	}
}

func TestCreateDuplicateEmailConflicts(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	_, err := repo.Create(ctx, NewUser{Email: "dup@x.com", PasswordHash: "h", Role: auth.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	// Different case, same address.
	_, err = repo.Create(ctx, NewUser{Email: "DUP@x.com", PasswordHash: "h2", Role: auth.RoleOperator})
	if err == nil {
		t.Fatal("duplicate email should conflict")
	}
	if apperr.Kind(err) != apperr.KindConflict {
		t.Errorf("kind = %v, want KindConflict", apperr.Kind(err))
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, NewUser{Email: "x@y.com", PasswordHash: "h", Role: "root"}); apperr.Kind(err) != apperr.KindInvalid {
		t.Errorf("invalid role should be KindInvalid, got %v", apperr.Kind(err))
	}
}

func TestGetByEmailNotFound(t *testing.T) {
	repo := newRepo(t)
	if _, err := repo.GetByEmail(context.Background(), "nobody@x.com"); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound", apperr.Kind(err))
	}
}

func TestGetByIDNotFound(t *testing.T) {
	repo := newRepo(t)
	missing := "00000000-0000-0000-0000-000000000000"
	if _, err := repo.GetByID(context.Background(), missing); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound", apperr.Kind(err))
	}
}

func TestListReturnsAllUsers(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	for _, e := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		if _, err := repo.Create(ctx, NewUser{Email: e, PasswordHash: "h", Role: auth.RoleViewer}); err != nil {
			t.Fatal(err)
		}
	}
	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("len = %d, want 3", len(users))
	}
}

func TestSetDisabled(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	u, _ := repo.Create(ctx, NewUser{Email: "d@x.com", PasswordHash: "h", Role: auth.RoleOperator})
	if err := repo.SetDisabled(ctx, u.ID, true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if !got.Disabled {
		t.Error("user should be disabled")
	}
	if !got.UpdatedAt.After(u.UpdatedAt) {
		t.Error("updated_at should advance on disable")
	}
}

func TestUpdatePasswordHash(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	u, _ := repo.Create(ctx, NewUser{Email: "p@x.com", PasswordHash: "old", Role: auth.RoleViewer})
	if err := repo.UpdatePasswordHash(ctx, u.ID, "new-hash"); err != nil {
		t.Fatalf("UpdatePasswordHash: %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if got.PasswordHash != "new-hash" {
		t.Errorf("hash = %q, want new-hash", got.PasswordHash)
	}
}

func TestUpdatesOnMissingUserReturnNotFound(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	missing := "00000000-0000-0000-0000-000000000000"

	if err := repo.SetDisabled(ctx, missing, true); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("SetDisabled missing = %v, want KindNotFound", apperr.Kind(err))
	}
	if err := repo.UpdatePasswordHash(ctx, missing, "h"); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("UpdatePasswordHash missing = %v, want KindNotFound", apperr.Kind(err))
	}
}
