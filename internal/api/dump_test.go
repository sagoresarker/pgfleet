package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/instance"
)

// TestBuildDumpCmdHidesPassword verifies the pg_dump argv carries the
// non-secret connection params but NOT the password, and that the password is
// supplied via the PGPASSWORD environment variable instead (SEC-6/REG-3: the
// DSN-in-argv was visible in `ps`).
func TestBuildDumpCmdHidesPassword(t *testing.T) {
	const secret = "sup3r-s3cret-pw"
	dsn := "postgres://pguser:" + secret + "@db.internal:6543/appdb?sslmode=disable"

	cmd, err := buildDumpCmd(context.Background(), dsn)
	if err != nil {
		t.Fatalf("buildDumpCmd: %v", err)
	}

	argv := strings.Join(cmd.Args, " ")
	if strings.Contains(argv, secret) {
		t.Fatalf("password leaked into argv: %q", argv)
	}
	// Non-secret params must be present so pg_dump connects to the right place.
	for _, want := range []string{"db.internal", "6543", "pguser", "appdb"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %q: %q", want, argv)
		}
	}

	// Password must travel via PGPASSWORD in the command environment.
	var found bool
	for _, e := range cmd.Env {
		if e == "PGPASSWORD="+secret {
			found = true
		}
		// And the secret must not appear in any OTHER env entry by accident.
	}
	if !found {
		t.Fatalf("PGPASSWORD not set in cmd.Env: %v", cmd.Env)
	}
}

// TestBuildDumpCmdRejectsBadDSN surfaces a parse error rather than silently
// shipping a malformed connection.
func TestBuildDumpCmdRejectsBadDSN(t *testing.T) {
	if _, err := buildDumpCmd(context.Background(), "::::not a dsn::::"); err == nil {
		t.Fatalf("expected an error for a malformed DSN")
	}
}

// TestDumpLogsStderrOnFailure verifies that when pg_dump fails mid-stream the
// handler logs the stderr at error level instead of silently swallowing it
// (SEC-7/REG-4). We point the handler at a fake "pg_dump" that prints to stderr
// and exits non-zero.
func TestDumpLogsStderrOnFailure(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := NewDumpHandler(
		fakeExecLookup{inst: instance.Instance{ID: "i1", Name: "myinst"}},
		func(context.Context, string) (string, error) {
			return "postgres://u:p@h:5432/d", nil
		},
	).WithLogger(logger)

	// Inject a command builder that runs a shell emitting stderr then failing,
	// without writing any stdout (so nothing is committed to the response body
	// before failure — exercises the "log error" path).
	h.buildCmd = func(ctx context.Context, _ string) (*dumpCmd, error) {
		return newShellDumpCmd(ctx, ">&2 echo 'pg_dump: connection refused'; exit 1"), nil
	}

	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodGet, "/", nil), "i1")
	h.Get(rr, req)

	logs := logBuf.String()
	if !strings.Contains(logs, "connection refused") {
		t.Fatalf("stderr was not logged on pg_dump failure; logs=%q", logs)
	}
	if !strings.Contains(strings.ToLower(logs), "error") && !strings.Contains(logs, "ERROR") {
		t.Fatalf("failure was not logged at error level; logs=%q", logs)
	}
}

// TestDumpSucceedsStreamsBody verifies a successful dump streams stdout to the
// client body and does not log an error.
func TestDumpSucceedsStreamsBody(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := NewDumpHandler(
		fakeExecLookup{inst: instance.Instance{ID: "i1", Name: "myinst"}},
		func(context.Context, string) (string, error) { return "postgres://u:p@h:5432/d", nil },
	).WithLogger(logger)
	h.buildCmd = func(ctx context.Context, _ string) (*dumpCmd, error) {
		return newShellDumpCmd(ctx, "echo '-- dump output'"), nil
	}

	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodGet, "/", nil), "i1")
	h.Get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "dump output") {
		t.Fatalf("body did not contain dump output: %q", rr.Body.String())
	}
	if strings.Contains(strings.ToUpper(logBuf.String()), "ERROR") {
		t.Fatalf("unexpected error log on success: %q", logBuf.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/sql" {
		t.Errorf("Content-Type = %q, want application/sql", ct)
	}
}
