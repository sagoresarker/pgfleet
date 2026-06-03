package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/provision"
)

type fakeBackupRunner struct {
	runs chan [2]string
	list []backup.Backup
}

func newFakeBackupRunner() *fakeBackupRunner {
	return &fakeBackupRunner{runs: make(chan [2]string, 4)}
}

func (f *fakeBackupRunner) Run(_ context.Context, instanceID, backupType string) error {
	f.runs <- [2]string{instanceID, backupType}
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
	h := NewBackupsHandler(runner, restorer, nil)
	r := chi.NewRouter()
	r.Post("/api/v1/instances/{id}/backups", h.Create)
	r.Get("/api/v1/instances/{id}/backups", h.List)
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
		if got[0] != "i1" || got[1] != "full" {
			t.Errorf("run = %v, want [i1 full]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backup was not triggered")
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

func TestRestoreRejectsTimeWithoutTarget(t *testing.T) {
	h := mountBackups(newFakeBackupRunner(), newFakeRestorer())
	rr := postJSON(t, h, "/api/v1/instances/i1/restore", `{"type":"time"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}
