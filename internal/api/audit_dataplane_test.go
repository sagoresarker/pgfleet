package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/audit"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// recordingAudit is a thread-safe fake AuditRecorder that captures entries.
type recordingAudit struct {
	mu      sync.Mutex
	entries []audit.Entry
	err     error
}

func (r *recordingAudit) Record(_ context.Context, e audit.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return r.err
}

func (r *recordingAudit) only(t *testing.T) audit.Entry {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) != 1 {
		t.Fatalf("got %d audit entries, want exactly 1: %+v", len(r.entries), r.entries)
	}
	return r.entries[0]
}

// TestSQLAudited verifies a SQL invocation emits an audit record naming the
// action and instance target (SEC-9), even though the query itself fails to
// connect (the privileged ATTEMPT is what we audit).
func TestSQLAudited(t *testing.T) {
	rec := &recordingAudit{}
	dsn := func(context.Context, string) (string, error) {
		return "postgres://u:p@127.0.0.1:1/x?connect_timeout=1", nil
	}
	h := NewSQLHandler(dsn).WithAudit(rec)
	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"query":"select 1"}`)), "i1")
	h.Run(rr, req)

	e := rec.only(t)
	if e.Action != "instance.sql" {
		t.Errorf("action = %q, want instance.sql", e.Action)
	}
	if e.Target != "i1" {
		t.Errorf("target = %q, want i1", e.Target)
	}
}

// TestExecAudited verifies an exec invocation emits an audit record.
func TestExecAudited(t *testing.T) {
	rec := &recordingAudit{}
	rt := execRuntimeFunc(func(context.Context, string, []string) (docker.ExecResult, error) {
		return docker.ExecResult{ExitCode: 0}, nil
	})
	h := NewExecHandler(fakeExecLookup{inst: instance.Instance{ID: "i1", ContainerID: "c1"}}, rt).WithAudit(rec)
	rr := httptest.NewRecorder()
	h.Run(rr, execRequestBody("i1", `{"command":["ls"]}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	e := rec.only(t)
	if e.Action != "instance.exec" {
		t.Errorf("action = %q, want instance.exec", e.Action)
	}
	if e.Target != "i1" {
		t.Errorf("target = %q, want i1", e.Target)
	}
}

// TestDumpAudited verifies a dump invocation emits an audit record.
func TestDumpAudited(t *testing.T) {
	rec := &recordingAudit{}
	h := NewDumpHandler(
		fakeExecLookup{inst: instance.Instance{ID: "i1", Name: "n1"}},
		func(context.Context, string) (string, error) { return "postgres://u:p@h:5432/d", nil },
	).WithAudit(rec)
	h.buildCmd = func(ctx context.Context, _ string) (*dumpCmd, error) {
		return newShellDumpCmd(ctx, "echo '-- ok'"), nil
	}
	rr := httptest.NewRecorder()
	h.Get(rr, withID(httptest.NewRequest(http.MethodGet, "/", nil), "i1"))

	e := rec.only(t)
	if e.Action != "instance.dump" {
		t.Errorf("action = %q, want instance.dump", e.Action)
	}
	if e.Target != "i1" {
		t.Errorf("target = %q, want i1", e.Target)
	}
}

// TestExecNotAuditedWhenGuardFails verifies we do NOT audit when the request is
// rejected before the privileged action runs (e.g. no container) — auditing a
// rejected request would be noise.
func TestExecNotAuditedWhenGuardFails(t *testing.T) {
	rec := &recordingAudit{}
	rt := execRuntimeFunc(func(context.Context, string, []string) (docker.ExecResult, error) {
		return docker.ExecResult{}, nil
	})
	h := NewExecHandler(fakeExecLookup{inst: instance.Instance{ID: "i1"}}, rt).WithAudit(rec)
	rr := httptest.NewRecorder()
	h.Run(rr, execRequestBody("i1", `{"command":["ls"]}`))

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	rec.mu.Lock()
	n := len(rec.entries)
	rec.mu.Unlock()
	if n != 0 {
		t.Fatalf("got %d audit entries, want 0 (request was rejected before action)", n)
	}
}
