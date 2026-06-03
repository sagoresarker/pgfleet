package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// gatingRouter wires the three privileged data-plane handlers behind a real
// issuer so the RequireAction gates run. The handlers use fakes so a request
// that passes the gate fails for an unrelated reason (never 403).
func gatingRouter(t *testing.T) (http.Handler, *auth.Issuer) {
	t.Helper()
	issuer, err := auth.NewIssuer([]byte("gating-secret-at-least-32-bytes!!"), time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	lookup := fakeExecLookup{inst: instance.Instance{ID: "i1", Name: "n1", ContainerID: "c1"}}
	rt := execRuntimeFunc(func(context.Context, string, []string) (docker.ExecResult, error) {
		return docker.ExecResult{ExitCode: 0}, nil
	})
	dsn := func(context.Context, string) (string, error) { return "postgres://u:p@127.0.0.1:1/x", nil }

	router := NewRouter(Deps{
		Issuer: issuer,
		SQL:    NewSQLHandler(dsn),
		Exec:   NewExecHandler(lookup, rt),
		Dump:   NewDumpHandler(lookup, dsn),
	})
	return router, issuer
}

func gatingReq(t *testing.T, h http.Handler, issuer *auth.Issuer, role auth.Role, method, path, body string) int {
	t.Helper()
	tok, err := issuer.Issue("u1", "u1@example.com", role)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr.Code
}

// TestPrivilegedEndpointsDenyViewer locks in the SEC-1 decision: the three
// privileged data-plane endpoints (sql, exec, dump) are ALL gated above the
// read-only viewer level — none are reachable by a viewer. This is the
// consistency invariant; exec is additionally gated stricter (write), which is
// documented at the route.
func TestPrivilegedEndpointsDenyViewer(t *testing.T) {
	h, issuer := gatingRouter(t)
	cases := []struct {
		name, method, path, body string
	}{
		{"sql", http.MethodPost, "/api/v1/instances/i1/sql", `{"query":"select 1"}`},
		{"exec", http.MethodPost, "/api/v1/instances/i1/exec", `{"command":["ls"]}`},
		{"dump", http.MethodGet, "/api/v1/instances/i1/dump", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := gatingReq(t, h, issuer, auth.RoleViewer, c.method, c.path, c.body)
			if code != http.StatusForbidden {
				t.Fatalf("viewer %s %s: status %d, want 403", c.method, c.path, code)
			}
		})
	}
}

// TestPrivilegedEndpointsAllowOperator confirms an operator passes every gate
// (the request then fails downstream for an unrelated reason, never 403).
func TestPrivilegedEndpointsAllowOperator(t *testing.T) {
	h, issuer := gatingRouter(t)
	cases := []struct {
		name, method, path, body string
	}{
		{"sql", http.MethodPost, "/api/v1/instances/i1/sql", `{"query":"select 1"}`},
		{"exec", http.MethodPost, "/api/v1/instances/i1/exec", `{"command":["ls"]}`},
		{"dump", http.MethodGet, "/api/v1/instances/i1/dump", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := gatingReq(t, h, issuer, auth.RoleOperator, c.method, c.path, c.body)
			if code == http.StatusForbidden {
				t.Fatalf("operator %s %s was denied (403); should pass the gate", c.method, c.path)
			}
		})
	}
}
