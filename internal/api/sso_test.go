package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

type fakeSSOStore struct {
	byEmail map[string]user.User
	created []user.NewUser
	nextID  int
}

func newFakeSSOStore() *fakeSSOStore { return &fakeSSOStore{byEmail: map[string]user.User{}} }

func (f *fakeSSOStore) GetByEmail(_ context.Context, email string) (user.User, error) {
	u, ok := f.byEmail[user.NormalizeEmail(email)]
	if !ok {
		return user.User{}, apperr.New(apperr.KindNotFound, "not found")
	}
	return u, nil
}

func (f *fakeSSOStore) Create(_ context.Context, in user.NewUser) (user.User, error) {
	if err := in.Validate(); err != nil {
		return user.User{}, err
	}
	f.created = append(f.created, in)
	f.nextID++
	u := user.User{ID: string(rune('a' + f.nextID)), Email: user.NormalizeEmail(in.Email), Role: in.Role}
	f.byEmail[u.Email] = u
	return u, nil
}

type fakeIssuer struct{ role auth.Role }

func (f *fakeIssuer) Issue(_, _ string, role auth.Role) (string, error) {
	f.role = role
	return "tok-" + string(role), nil
}

func ssoRequest(header, email, groupsHeader, groups string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sso", nil)
	if email != "" {
		req.Header.Set(header, email)
	}
	if groups != "" {
		req.Header.Set(groupsHeader, groups)
	}
	return req
}

func TestSSOExchangeNoHeaderRejected(t *testing.T) {
	h := NewSSOHandler(newFakeSSOStore(), &fakeIssuer{}, SSOConfig{EmailHeader: "Remote-Email"})
	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "", "", ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestSSOExchangeSharedSecretRequired(t *testing.T) {
	store := newFakeSSOStore()
	store.byEmail["op@x.com"] = user.User{ID: "u1", Email: "op@x.com", Role: auth.RoleOperator}
	h := NewSSOHandler(store, &fakeIssuer{}, SSOConfig{EmailHeader: "Remote-Email", SharedSecret: "s3cr3t"})

	// A valid identity header but NO/​wrong proxy secret must be rejected — this is
	// the off-proxy forgery scenario.
	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "op@x.com", "", ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret: status = %d, want 401", rr.Code)
	}
	rr = httptest.NewRecorder()
	bad := ssoRequest("Remote-Email", "op@x.com", "", "")
	bad.Header.Set(ssoSecretHeader, "wrong")
	h.Exchange(rr, bad)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: status = %d, want 401", rr.Code)
	}

	// With the correct secret it succeeds.
	rr = httptest.NewRecorder()
	ok := ssoRequest("Remote-Email", "op@x.com", "", "")
	ok.Header.Set(ssoSecretHeader, "s3cr3t")
	h.Exchange(rr, ok)
	if rr.Code != http.StatusOK {
		t.Fatalf("correct secret: status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestSSOExchangeExistingUser(t *testing.T) {
	store := newFakeSSOStore()
	store.byEmail["op@x.com"] = user.User{ID: "u1", Email: "op@x.com", Role: auth.RoleOperator}
	iss := &fakeIssuer{}
	h := NewSSOHandler(store, iss, SSOConfig{EmailHeader: "Remote-Email"})

	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "op@x.com", "", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if iss.role != auth.RoleOperator {
		t.Errorf("issued role = %q, want operator", iss.role)
	}
	if len(store.created) != 0 {
		t.Error("an existing user must not be re-provisioned")
	}
}

func TestSSOAutoProvisionViewerByDefault(t *testing.T) {
	store := newFakeSSOStore()
	h := NewSSOHandler(store, &fakeIssuer{}, SSOConfig{EmailHeader: "Remote-Email", AutoProvision: true})

	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "new@x.com", "", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if len(store.created) != 1 || store.created[0].Role != auth.RoleViewer {
		t.Errorf("auto-provisioned role = %v, want viewer (least privilege)", store.created)
	}
}

func TestSSOAutoProvisionAdminFromGroup(t *testing.T) {
	store := newFakeSSOStore()
	h := NewSSOHandler(store, &fakeIssuer{}, SSOConfig{
		EmailHeader: "Remote-Email", GroupsHeader: "Remote-Groups", AutoProvision: true,
		AdminGroup: "pgfleet-admins",
	})

	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "boss@x.com", "Remote-Groups", "staff,pgfleet-admins"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if len(store.created) != 1 || store.created[0].Role != auth.RoleAdmin {
		t.Errorf("auto-provisioned role = %v, want admin (from group)", store.created)
	}
}

func TestSSONoAutoProvisionRejectsUnknown(t *testing.T) {
	store := newFakeSSOStore()
	h := NewSSOHandler(store, &fakeIssuer{}, SSOConfig{EmailHeader: "Remote-Email", AutoProvision: false})

	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "ghost@x.com", "", ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (no provisioned account)", rr.Code)
	}
	if len(store.created) != 0 {
		t.Error("must not provision when auto-provision is off")
	}
}

func TestSSODisabledUserRejected(t *testing.T) {
	store := newFakeSSOStore()
	store.byEmail["x@x.com"] = user.User{ID: "u1", Email: "x@x.com", Role: auth.RoleViewer, Disabled: true}
	h := NewSSOHandler(store, &fakeIssuer{}, SSOConfig{EmailHeader: "Remote-Email"})

	rr := httptest.NewRecorder()
	h.Exchange(rr, ssoRequest("Remote-Email", "x@x.com", "", ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (disabled)", rr.Code)
	}
}
