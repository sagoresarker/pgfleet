package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

type fakeStore struct {
	users     []user.User
	created   []user.NewUser
	createErr error
}

func (f *fakeStore) List(context.Context) ([]user.User, error) { return f.users, nil }

func (f *fakeStore) Create(_ context.Context, in user.NewUser) (user.User, error) {
	if f.createErr != nil {
		return user.User{}, f.createErr
	}
	f.created = append(f.created, in)
	return user.User{ID: "new", Email: in.Email, Role: in.Role}, nil
}

func TestEnsureAdminCreatesOnEmptyStore(t *testing.T) {
	store := &fakeStore{}
	created, err := EnsureAdmin(context.Background(), store, "root@x.com", "strong-password")
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if !created {
		t.Fatal("expected admin to be created")
	}
	if len(store.created) != 1 {
		t.Fatalf("expected 1 created, got %d", len(store.created))
	}
	got := store.created[0]
	if got.Role != auth.RoleAdmin {
		t.Errorf("role = %v, want admin", got.Role)
	}
	if got.PasswordHash == "strong-password" || got.PasswordHash == "" {
		t.Errorf("password should be hashed, got %q", got.PasswordHash)
	}
}

func TestEnsureAdminNoOpWhenUsersExist(t *testing.T) {
	store := &fakeStore{users: []user.User{{ID: "u1", Email: "someone@x.com"}}}
	created, err := EnsureAdmin(context.Background(), store, "root@x.com", "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("should not create admin when users already exist")
	}
	if len(store.created) != 0 {
		t.Error("Create should not be called")
	}
}

func TestEnsureAdminNoOpWithoutCredentials(t *testing.T) {
	for _, tc := range []struct{ email, pw string }{
		{"", ""},
		{"root@x.com", ""},
		{"", "strong-password"},
	} {
		store := &fakeStore{}
		created, err := EnsureAdmin(context.Background(), store, tc.email, tc.pw)
		if err != nil {
			t.Fatalf("EnsureAdmin(%q,%q): %v", tc.email, tc.pw, err)
		}
		if created || len(store.created) != 0 {
			t.Errorf("EnsureAdmin(%q,%q) should be a no-op", tc.email, tc.pw)
		}
	}
}

func TestEnsureAdminPropagatesCreateError(t *testing.T) {
	store := &fakeStore{createErr: errors.New("db down")}
	if _, err := EnsureAdmin(context.Background(), store, "root@x.com", "strong-password"); err == nil {
		t.Error("expected create error to propagate")
	}
}
