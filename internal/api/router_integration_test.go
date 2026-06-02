//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
	"github.com/sagoresarker/pgfleet/internal/user"
)

// fullStack wires the real repository + issuer + handlers behind the router.
func fullStack(t *testing.T) (http.Handler, *user.Repository) {
	pool, _ := testsupport.MigratedPool(t)
	repo := user.NewRepository(pool)
	issuer := auth.NewIssuer([]byte("integration-secret"), time.Hour)

	router := NewRouter(Deps{
		Issuer: issuer,
		Auth:   NewAuthHandler(repo, issuer, nil),
		Users:  NewUsersHandler(repo, nil),
	})
	return router, repo
}

func seedUser(t *testing.T, repo *user.Repository, email, password string, role auth.Role) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(context.Background(), user.NewUser{Email: email, PasswordHash: hash, Role: role}); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
}

func login(t *testing.T, h http.Handler, email, password string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"`+email+`","password":"`+password+`"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("login %s: status %d (%s)", email, rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Token
}

func createUserReq(h http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestFullStackAdminCanManageUsers(t *testing.T) {
	h, repo := fullStack(t)
	seedUser(t, repo, "admin@x.com", "admin-password", auth.RoleAdmin)

	token := login(t, h, "admin@x.com", "admin-password")
	rr := createUserReq(h, token, `{"email":"new@x.com","password":"new-password","role":"operator"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("admin create user status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}

	// The new user can now log in.
	_ = login(t, h, "new@x.com", "new-password")
}

func TestFullStackViewerForbiddenFromUserManagement(t *testing.T) {
	h, repo := fullStack(t)
	seedUser(t, repo, "viewer@x.com", "viewer-password", auth.RoleViewer)

	token := login(t, h, "viewer@x.com", "viewer-password")
	rr := createUserReq(h, token, `{"email":"x@x.com","password":"some-password","role":"viewer"}`)
	if rr.Code != http.StatusForbidden {
		t.Errorf("viewer create user status = %d, want 403", rr.Code)
	}
}

func TestFullStackUnauthenticatedRejected(t *testing.T) {
	h, _ := fullStack(t)
	rr := createUserReq(h, "", `{"email":"x@x.com","password":"some-password","role":"viewer"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-token create user status = %d, want 401", rr.Code)
	}
}

func TestFullStackLogout(t *testing.T) {
	h, repo := fullStack(t)
	seedUser(t, repo, "admin@x.com", "admin-password", auth.RoleAdmin)
	token := login(t, h, "admin@x.com", "admin-password")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("logout status = %d, want 204", rr.Code)
	}
}
