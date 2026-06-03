package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

type fakeUserAdminStore struct {
	users    map[string]user.User
	created  []user.NewUser
	disabled map[string]bool
	conflict bool
	nextID   int
}

func newFakeAdminStore() *fakeUserAdminStore {
	return &fakeUserAdminStore{users: map[string]user.User{}, disabled: map[string]bool{}}
}

func (f *fakeUserAdminStore) Create(_ context.Context, in user.NewUser) (user.User, error) {
	if err := in.Validate(); err != nil {
		return user.User{}, err
	}
	if f.conflict {
		return user.User{}, apperr.New(apperr.KindConflict, "exists")
	}
	f.created = append(f.created, in)
	f.nextID++
	u := user.User{ID: string(rune('a' + f.nextID)), Email: user.NormalizeEmail(in.Email), Role: in.Role, PasswordHash: in.PasswordHash}
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeUserAdminStore) List(_ context.Context) ([]user.User, error) {
	var out []user.User
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserAdminStore) SetDisabled(_ context.Context, id string, disabled bool) error {
	if _, ok := f.users[id]; !ok {
		return apperr.New(apperr.KindNotFound, "not found")
	}
	f.disabled[id] = disabled
	return nil
}

// mountUsers wires the users handler under a chi router (for URL params).
func mountUsers(h *UsersHandler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/users", h.Create)
	r.Get("/api/v1/users", h.List)
	r.Post("/api/v1/users/{id}/disable", h.Disable)
	r.Post("/api/v1/users/{id}/enable", h.Enable)
	return r
}

func TestCreateUserHashesPasswordAndReturns201(t *testing.T) {
	store := newFakeAdminStore()
	h := mountUsers(NewUsersHandler(store, nil))

	rr := postJSON(t, h, "/api/v1/users", `{"email":"new@x.com","password":"s3cret-pass","role":"operator"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("expected 1 created user, got %d", len(store.created))
	}
	created := store.created[0]
	if created.PasswordHash == "s3cret-pass" || !strings.HasPrefix(created.PasswordHash, "$argon2id$") {
		t.Errorf("password was not hashed: %q", created.PasswordHash)
	}

	var resp struct {
		User userPayload `json:"user"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.User.Role != "operator" {
		t.Errorf("role = %q, want operator", resp.User.Role)
	}
}

func TestCreateUserConflict(t *testing.T) {
	store := newFakeAdminStore()
	store.conflict = true
	h := mountUsers(NewUsersHandler(store, nil))

	rr := postJSON(t, h, "/api/v1/users", `{"email":"dup@x.com","password":"longenough","role":"viewer"}`)
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rr.Code)
	}
}

func TestCreateUserValidation(t *testing.T) {
	store := newFakeAdminStore()
	h := mountUsers(NewUsersHandler(store, nil))

	bodies := []string{
		`{"email":"x@x.com","password":"longenough","role":"root"}`, // bad role
		`{"email":"x@x.com","password":"short","role":"viewer"}`,    // weak password
		`{"email":"","password":"longenough","role":"viewer"}`,      // missing email
		`not json`,
	}
	for _, b := range bodies {
		rr := postJSON(t, h, "/api/v1/users", b)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", b, rr.Code)
		}
	}
}

func TestListUsers(t *testing.T) {
	store := newFakeAdminStore()
	_, _ = store.Create(context.Background(), user.NewUser{Email: "a@x.com", PasswordHash: "$argon2id$h", Role: auth.RoleViewer})
	h := mountUsers(NewUsersHandler(store, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp struct {
		Users []userPayload `json:"users"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Users) != 1 {
		t.Errorf("len = %d, want 1", len(resp.Users))
	}
}

func TestDisableAndEnableUser(t *testing.T) {
	store := newFakeAdminStore()
	u, _ := store.Create(context.Background(), user.NewUser{Email: "a@x.com", PasswordHash: "$argon2id$h", Role: auth.RoleViewer})
	h := mountUsers(NewUsersHandler(store, nil))

	rr := postJSON(t, h, "/api/v1/users/"+u.ID+"/disable", ``)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d, want 204", rr.Code)
	}
	if !store.disabled[u.ID] {
		t.Error("user should be disabled")
	}

	rr = postJSON(t, h, "/api/v1/users/"+u.ID+"/enable", ``)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("enable status = %d, want 204", rr.Code)
	}
	if store.disabled[u.ID] {
		t.Error("user should be enabled")
	}
}

func TestDisableMissingUserIs404(t *testing.T) {
	store := newFakeAdminStore()
	h := mountUsers(NewUsersHandler(store, nil))
	rr := postJSON(t, h, "/api/v1/users/ghost/disable", ``)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// TestDisableLastAdminRejected — disabling the only active admin would lock
// everyone out (Login rejects disabled accounts), so it must be refused.
func TestDisableLastAdminRejected(t *testing.T) {
	store := newFakeAdminStore()
	store.users["admin1"] = user.User{ID: "admin1", Email: "admin@x.com", Role: auth.RoleAdmin}
	h := mountUsers(NewUsersHandler(store, nil))

	rr := postJSON(t, h, "/api/v1/users/admin1/disable", ``)
	if rr.Code == http.StatusNoContent {
		t.Fatalf("disabling the last admin must be rejected, got 204")
	}
	if store.disabled["admin1"] {
		t.Error("last admin must not have been disabled")
	}
}

// TestDisableAdminWithAnotherActiveAdminAllowed — with a second active admin,
// disabling one is fine (no lockout).
func TestDisableAdminWithAnotherActiveAdminAllowed(t *testing.T) {
	store := newFakeAdminStore()
	store.users["admin1"] = user.User{ID: "admin1", Email: "a1@x.com", Role: auth.RoleAdmin}
	store.users["admin2"] = user.User{ID: "admin2", Email: "a2@x.com", Role: auth.RoleAdmin}
	h := mountUsers(NewUsersHandler(store, nil))

	rr := postJSON(t, h, "/api/v1/users/admin1/disable", ``)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d, want 204", rr.Code)
	}
	if !store.disabled["admin1"] {
		t.Error("admin1 should have been disabled")
	}
}

// TestDisableSelfRejected — an admin cannot disable their own account even if
// other admins exist (avoids the foot-gun and a confusing session).
func TestDisableSelfRejected(t *testing.T) {
	store := newFakeAdminStore()
	store.users["admin1"] = user.User{ID: "admin1", Email: "a1@x.com", Role: auth.RoleAdmin}
	store.users["admin2"] = user.User{ID: "admin2", Email: "a2@x.com", Role: auth.RoleAdmin}
	handler := mountUsers(NewUsersHandler(store, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/admin1/disable", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{UserID: "admin1", Role: auth.RoleAdmin}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code == http.StatusNoContent {
		t.Fatalf("self-disable must be rejected, got 204")
	}
	if store.disabled["admin1"] {
		t.Error("self-disable must not have taken effect")
	}
}
