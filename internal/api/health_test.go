package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHealthzAlwaysOK(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error {
		return errors.New("db down") // liveness must not depend on readiness
	}})

	w := doGet(t, r, "/healthz")
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", w.Code)
	}
}

func TestReadyzOKWhenCheckPasses(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error { return nil }})

	w := doGet(t, r, "/readyz")
	if w.Code != http.StatusOK {
		t.Fatalf("/readyz status = %d, want 200", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}

func TestReadyzUnavailableWhenCheckFails(t *testing.T) {
	r := NewRouter(Deps{Ready: func(context.Context) error {
		return errors.New("db down")
	}})

	w := doGet(t, r, "/readyz")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503", w.Code)
	}
}

func TestReadyzDefaultsToOKWhenNoChecker(t *testing.T) {
	r := NewRouter(Deps{}) // no Ready func provided

	w := doGet(t, r, "/readyz")
	if w.Code != http.StatusOK {
		t.Fatalf("/readyz with nil checker = %d, want 200", w.Code)
	}
}
