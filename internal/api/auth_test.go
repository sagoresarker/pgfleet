package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/user"
)

// --- fakes ---

type fakeUserStore struct {
	byEmail     map[string]user.User
	updatedHash map[string]string
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{byEmail: map[string]user.User{}, updatedHash: map[string]string{}}
}

func (f *fakeUserStore) GetByEmail(_ context.Context, email string) (user.User, error) {
	u, ok := f.byEmail[user.NormalizeEmail(email)]
	if !ok {
		return user.User{}, apperr.New(apperr.KindNotFound, "not found")
	}
	return u, nil
}

func (f *fakeUserStore) UpdatePasswordHash(_ context.Context, id, hash string) error {
	f.updatedHash[id] = hash
	return nil
}

func (f *fakeUserStore) add(u user.User) { f.byEmail[user.NormalizeEmail(u.Email)] = u }

// makeWeakHash builds a valid argon2id PHC hash with deliberately weak params,
// so VerifyPassword accepts it but NeedsRehash flags it.
func makeWeakHash(password string) string {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte(password), salt, 1, 8, 1, 32)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=8,t=1,p=1$%s$%s",
		argon2.Version, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// testIssuer builds an Issuer with a 32-byte secret (meets MinSecretLen).
func testIssuer() *auth.Issuer {
	iss, err := auth.NewIssuer([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	if err != nil {
		panic(err)
	}
	return iss
}

func newAuthHandler(store UserAuthStore) *AuthHandler {
	return NewAuthHandler(store, testIssuer(), nil)
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// --- tests ---

func TestLoginSuccessReturnsVerifiableToken(t *testing.T) {
	store := newFakeUserStore()
	hash, _ := auth.HashPassword("hunter2")
	store.add(user.User{ID: "u1", Email: "a@b.com", PasswordHash: hash, Role: auth.RoleAdmin})

	issuer := testIssuer()
	h := http.HandlerFunc(NewAuthHandler(store, issuer, nil).Login)

	rr := postJSON(t, h, "/login", `{"email":"a@b.com","password":"hunter2"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}

	var resp struct {
		Token string `json:"token"`
		User  struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	claims, err := issuer.Verify(resp.Token)
	if err != nil {
		t.Fatalf("issued token does not verify: %v", err)
	}
	if claims.UserID != "u1" || claims.Role != auth.RoleAdmin {
		t.Errorf("claims = %+v", claims)
	}
	if resp.User.Email != "a@b.com" || resp.User.Role != "admin" {
		t.Errorf("user payload = %+v", resp.User)
	}
}

func TestLoginUnknownEmailIs401(t *testing.T) {
	h := http.HandlerFunc(newAuthHandler(newFakeUserStore()).Login)
	rr := postJSON(t, h, "/login", `{"email":"nobody@x.com","password":"x"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestLoginWrongPasswordIs401(t *testing.T) {
	store := newFakeUserStore()
	hash, _ := auth.HashPassword("right")
	store.add(user.User{ID: "u1", Email: "a@b.com", PasswordHash: hash, Role: auth.RoleViewer})

	h := http.HandlerFunc(newAuthHandler(store).Login)
	rr := postJSON(t, h, "/login", `{"email":"a@b.com","password":"wrong"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestLoginDisabledUserIsForbidden(t *testing.T) {
	store := newFakeUserStore()
	hash, _ := auth.HashPassword("pw")
	store.add(user.User{ID: "u1", Email: "a@b.com", PasswordHash: hash, Role: auth.RoleViewer, Disabled: true})

	h := http.HandlerFunc(newAuthHandler(store).Login)
	rr := postJSON(t, h, "/login", `{"email":"a@b.com","password":"pw"}`)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestLoginRejectsBadInput(t *testing.T) {
	h := http.HandlerFunc(newAuthHandler(newFakeUserStore()).Login)
	for _, body := range []string{`not json`, `{"email":"a@b.com"}`, `{"password":"x"}`, `{}`} {
		rr := postJSON(t, h, "/login", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", body, rr.Code)
		}
	}
}

func TestLoginUpgradesWeakHash(t *testing.T) {
	store := newFakeUserStore()
	store.add(user.User{ID: "u1", Email: "a@b.com", PasswordHash: makeWeakHash("pw"), Role: auth.RoleViewer})

	h := http.HandlerFunc(newAuthHandler(store).Login)
	rr := postJSON(t, h, "/login", `{"email":"a@b.com","password":"pw"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	newHash, ok := store.updatedHash["u1"]
	if !ok {
		t.Fatal("expected weak hash to be upgraded on login")
	}
	if auth.NeedsRehash(newHash) {
		t.Error("upgraded hash should not need rehashing")
	}
}
