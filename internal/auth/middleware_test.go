package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler(seen *Claims) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := ClaimsFromContext(r.Context()); ok && seen != nil {
			*seen = *c
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddlewareRejectsMissingHeader(t *testing.T) {
	iss := mustIssuer(t, testSecret, time.Hour)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	iss.Authenticate(okHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("missing header status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddlewareRejectsMalformedHeader(t *testing.T) {
	iss := mustIssuer(t, testSecret, time.Hour)
	for _, h := range []string{"Bearer", "Token abc", "abc", "Bearer "} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", h)
		iss.Authenticate(okHandler(nil)).ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("header %q status = %d, want 401", h, rr.Code)
		}
	}
}

func TestAuthMiddlewareAcceptsValidTokenAndInjectsClaims(t *testing.T) {
	iss := mustIssuer(t, testSecret, time.Hour)
	token, _ := iss.Issue("u1", "u1@x.com", RoleOperator)

	var seen Claims
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	iss.Authenticate(okHandler(&seen)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", rr.Code)
	}
	if seen.UserID != "u1" || seen.Role != RoleOperator {
		t.Errorf("handler did not see injected claims: %+v", seen)
	}
}

func TestRequireActionAllowsSufficientRole(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Role: RoleOperator}))

	RequireAction(ActionInstanceWrite)(okHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("operator instance.write status = %d, want 200", rr.Code)
	}
}

func TestRequireActionForbidsInsufficientRole(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Role: RoleViewer}))

	RequireAction(ActionInstanceWrite)(okHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("viewer instance.write status = %d, want 403", rr.Code)
	}
}

func TestRequireActionWithoutClaimsIs401(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)

	RequireAction(ActionInstanceRead)(okHandler(nil)).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no claims status = %d, want 401", rr.Code)
	}
}
