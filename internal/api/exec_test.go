package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// fakeExecLookup is a stub execInstanceLookup.
type fakeExecLookup struct {
	inst instance.Instance
	err  error
}

func (f fakeExecLookup) Get(context.Context, string) (instance.Instance, error) {
	return f.inst, f.err
}

// fakeExecRuntime is a stub execRuntime. It records the context it was called
// with so a test can assert a deadline was applied, and returns a fixed result.
type fakeExecRuntime struct {
	res         docker.ExecResult
	err         error
	gotCtx      context.Context
	hadDeadline bool
}

func (f *fakeExecRuntime) Exec(ctx context.Context, _ string, _ []string) (docker.ExecResult, error) {
	f.gotCtx = ctx
	_, f.hadDeadline = ctx.Deadline()
	return f.res, f.err
}

// withID attaches a chi route context carrying URLParam "id", matching how the
// router invokes these handlers.
func withID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func execRequestBody(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	return withID(req, id)
}

// TestExecAppliesTimeout verifies the handler bounds the runtime call with a
// deadline so a never-exiting command cannot hang a worker forever (SEC-5).
func TestExecAppliesTimeout(t *testing.T) {
	rt := &fakeExecRuntime{res: docker.ExecResult{ExitCode: 0, Stdout: "ok"}}
	h := NewExecHandler(fakeExecLookup{inst: instance.Instance{ID: "i1", ContainerID: "c1"}}, rt)

	rr := httptest.NewRecorder()
	h.Run(rr, execRequestBody("i1", `{"command":["echo","hi"]}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !rt.hadDeadline {
		t.Fatalf("runtime Exec was called without a context deadline; timeout not applied")
	}
}

// TestExecBoundsOutput verifies oversized stdout/stderr from the runtime is
// capped by the handler and flagged truncated, so a gigabyte of output cannot
// be echoed back wholesale (SEC-5 defense-in-depth at the handler).
func TestExecBoundsOutput(t *testing.T) {
	huge := strings.Repeat("a", 5<<20) // 5 MiB
	rt := &fakeExecRuntime{res: docker.ExecResult{ExitCode: 0, Stdout: huge, Stderr: huge}}
	h := NewExecHandler(fakeExecLookup{inst: instance.Instance{ID: "i1", ContainerID: "c1"}}, rt)

	rr := httptest.NewRecorder()
	h.Run(rr, execRequestBody("i1", `{"command":["cat","/dev/zero"]}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if len(body) > 4<<20 {
		t.Fatalf("response body = %d bytes; oversized output was not bounded", len(body))
	}
	if !strings.Contains(body, `"truncated":true`) {
		t.Fatalf("response did not flag truncated output: %s", body[:min(len(body), 200)])
	}
}

// TestExecNoContainer returns 409 and never calls the runtime.
func TestExecNoContainer(t *testing.T) {
	rt := &fakeExecRuntime{}
	called := false
	wrap := execRuntimeFunc(func(ctx context.Context, id string, cmd []string) (docker.ExecResult, error) {
		called = true
		return rt.Exec(ctx, id, cmd)
	})
	h := NewExecHandler(fakeExecLookup{inst: instance.Instance{ID: "i1"}}, wrap)

	rr := httptest.NewRecorder()
	h.Run(rr, execRequestBody("i1", `{"command":["ls"]}`))

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	if called {
		t.Fatalf("runtime was called for a container-less instance")
	}
}

// TestExecLookupError propagates a not-found from the lookup.
func TestExecLookupError(t *testing.T) {
	h := NewExecHandler(fakeExecLookup{err: errors.New("boom")}, &fakeExecRuntime{})
	rr := httptest.NewRecorder()
	h.Run(rr, execRequestBody("i1", `{"command":["ls"]}`))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}

// execRuntimeFunc adapts a func to the execRuntime interface.
type execRuntimeFunc func(ctx context.Context, id string, cmd []string) (docker.ExecResult, error)

func (f execRuntimeFunc) Exec(ctx context.Context, id string, cmd []string) (docker.ExecResult, error) {
	return f(ctx, id, cmd)
}
