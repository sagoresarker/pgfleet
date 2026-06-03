package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

type runWithCall struct {
	instanceID string
	backupType string
	opts       backup.RunOpts
}

type fakeBackupRunner struct {
	runs     chan runWithCall
	deletes  chan [2]string
	verifies chan string
	list     []backup.Backup
	delErr   error
	verErr   error
}

func newFakeBackupRunner() *fakeBackupRunner {
	return &fakeBackupRunner{
		runs:     make(chan runWithCall, 4),
		deletes:  make(chan [2]string, 4),
		verifies: make(chan string, 4),
	}
}

func (f *fakeBackupRunner) RunWith(_ context.Context, instanceID, backupType string, opts backup.RunOpts) error {
	f.runs <- runWithCall{instanceID, backupType, opts}
	return nil
}
func (f *fakeBackupRunner) Delete(_ context.Context, instanceID, label string) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deletes <- [2]string{instanceID, label}
	return nil
}
func (f *fakeBackupRunner) Verify(_ context.Context, instanceID string) error {
	if f.verErr != nil {
		return f.verErr
	}
	f.verifies <- instanceID
	return nil
}
func (f *fakeBackupRunner) List(context.Context, string) ([]backup.Backup, error) {
	return f.list, nil
}

type fakeRestorer struct {
	restored chan provision.RestoreOptions
}

func newFakeRestorer() *fakeRestorer {
	return &fakeRestorer{restored: make(chan provision.RestoreOptions, 4)}
}

func (f *fakeRestorer) Restore(_ context.Context, _ string, opts provision.RestoreOptions, _ provision.ProgressFunc) error {
	f.restored <- opts
	return nil
}

func mountBackups(runner BackupRunner, restorer Restorer) http.Handler {
	return mountBackupsAudited(runner, restorer, nil)
}

func mountBackupsAudited(runner BackupRunner, restorer Restorer, rec AuditRecorder) http.Handler {
	h := NewBackupsHandler(runner, restorer, rec)
	r := chi.NewRouter()
	r.Post("/api/v1/instances/{id}/backups", h.Create)
	r.Get("/api/v1/instances/{id}/backups", h.List)
	r.Delete("/api/v1/instances/{id}/backups/{label}", h.Delete)
	r.Post("/api/v1/instances/{id}/backups/verify", h.Verify)
	r.Post("/api/v1/instances/{id}/restore", h.Restore)
	return r
}

func TestCreateBackupReturns202AndRuns(t *testing.T) {
	runner := newFakeBackupRunner()
	h := mountBackups(runner, newFakeRestorer())

	rr := postJSON(t, h, "/api/v1/instances/i1/backups", `{"type":"full"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case got := <-runner.runs:
		if got.instanceID != "i1" || got.backupType != "full" {
			t.Errorf("run = %+v, want i1/full", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backup was not triggered")
	}
}

func TestCreateBackupWithAnnotation(t *testing.T) {
	runner := newFakeBackupRunner()
	h := mountBackups(runner, newFakeRestorer())

	rr := postJSON(t, h, "/api/v1/instances/i1/backups", `{"type":"full","annotation":"my note"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case got := <-runner.runs:
		if got.opts.Annotation != "my note" {
			t.Errorf("annotation = %q, want %q", got.opts.Annotation, "my note")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backup was not triggered")
	}
}

func TestVerifyReturns202AndRuns(t *testing.T) {
	runner := newFakeBackupRunner()
	h := mountBackups(runner, newFakeRestorer())

	rr := postJSON(t, h, "/api/v1/instances/i1/backups/verify", `{}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case got := <-runner.verifies:
		if got != "i1" {
			t.Errorf("verify instance = %q, want i1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("verify was not triggered")
	}
}

func TestCreateBackupRejectsBadType(t *testing.T) {
	h := mountBackups(newFakeBackupRunner(), newFakeRestorer())
	for _, body := range []string{`{"type":"bogus"}`, `{}`, `not json`} {
		rr := postJSON(t, h, "/api/v1/instances/i1/backups", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", body, rr.Code)
		}
	}
}

func TestListBackups(t *testing.T) {
	runner := newFakeBackupRunner()
	runner.list = []backup.Backup{{Label: "20260603-120000F", Type: "full", RepoSize: 512}}
	h := mountBackups(runner, newFakeRestorer())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/i1/backups", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Backups []map[string]any `json:"backups"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Backups) != 1 || resp.Backups[0]["label"] != "20260603-120000F" {
		t.Errorf("backups = %v", resp.Backups)
	}
}

func TestDeleteBackupReturns204AndDeletes(t *testing.T) {
	runner := newFakeBackupRunner()
	h := mountBackups(runner, newFakeRestorer())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/i1/backups/20260603-120000F", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	select {
	case got := <-runner.deletes:
		if got[0] != "i1" || got[1] != "20260603-120000F" {
			t.Errorf("delete = %v, want [i1 20260603-120000F]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete was not invoked")
	}
}

func TestDeleteBackupIsAudited(t *testing.T) {
	rec := &recordingAudit{}
	h := mountBackupsAudited(newFakeBackupRunner(), newFakeRestorer(), rec)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/i1/backups/L1", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	e := rec.only(t)
	if e.Action != "backup.delete" || e.Target != "i1/L1" {
		t.Errorf("audit entry = %+v, want action backup.delete target i1/L1", e)
	}
}

func TestDeleteBackupSurfacesRunnerError(t *testing.T) {
	runner := newFakeBackupRunner()
	runner.delErr = apperr.New(apperr.KindNotFound, "no such backup")
	h := mountBackups(runner, newFakeRestorer())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/i1/backups/missing", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestRestoreReturns202(t *testing.T) {
	restorer := newFakeRestorer()
	h := mountBackups(newFakeBackupRunner(), restorer)

	rr := postJSON(t, h, "/api/v1/instances/i1/restore", `{"type":"time","target":"2026-06-03 12:00:00+00"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case opts := <-restorer.restored:
		if opts.Type != "time" || opts.Target != "2026-06-03 12:00:00+00" {
			t.Errorf("restore opts = %+v", opts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restore was not triggered")
	}
}

func TestRestoreAcceptsDelta(t *testing.T) {
	restorer := newFakeRestorer()
	h := mountBackups(newFakeBackupRunner(), restorer)

	rr := postJSON(t, h, "/api/v1/instances/i1/restore", `{"delta":true}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case opts := <-restorer.restored:
		if !opts.Delta {
			t.Errorf("restore opts.Delta = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restore was not triggered")
	}
}

func TestRestoreRejectsTimeWithoutTarget(t *testing.T) {
	h := mountBackups(newFakeBackupRunner(), newFakeRestorer())
	rr := postJSON(t, h, "/api/v1/instances/i1/restore", `{"type":"time"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}
