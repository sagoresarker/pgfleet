package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/remotebackup"
)

// --- fakes ---

type fakeRemoteService struct {
	mu sync.Mutex

	captureErr    error
	captured      []remotebackup.RemoteConn
	entries       map[string]remotebackup.CatalogEntry
	seq           int
	restoreErr    error
	restoreCalled bool
	restoredDSN   string
	restoredID    string
}

func newFakeRemoteService() *fakeRemoteService {
	return &fakeRemoteService{entries: map[string]remotebackup.CatalogEntry{}}
}

func (f *fakeRemoteService) Capture(_ context.Context, c remotebackup.RemoteConn) (remotebackup.CatalogEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.captureErr != nil {
		return remotebackup.CatalogEntry{}, f.captureErr
	}
	f.captured = append(f.captured, c)
	f.seq++
	id := "dump-" + string(rune('0'+f.seq))
	e := remotebackup.CatalogEntry{
		ID:         id,
		ObjectKey:  "remote-dumps/remote-x.dump",
		SourceHost: "[REDACTED].com",
		SourceDB:   c.DBName,
		ServerMaj:  16,
		Size:       42,
		CreatedAt:  time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
	}
	f.entries[id] = e
	return e, nil
}

func (f *fakeRemoteService) List(_ context.Context) ([]remotebackup.CatalogEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]remotebackup.CatalogEntry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeRemoteService) GetEntry(_ context.Context, id string) (remotebackup.CatalogEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return remotebackup.CatalogEntry{}, apperr.New(apperr.KindNotFound, "remotebackup: dump not found")
	}
	return e, nil
}

func (f *fakeRemoteService) RestoreInto(_ context.Context, dumpID, targetDSN string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restoreCalled = true
	f.restoredID = dumpID
	f.restoredDSN = targetDSN
	return f.restoreErr
}

type fakeRemoteProvisioner struct {
	mu sync.Mutex

	provInstanceErr error
	provClusterErr  error
	waitErr         error
	dsn             string

	provisionedKind string
	provisionedSpec RemoteTargetSpec
	marked          chan string // kind:id:reason
	restoredSignal  chan struct{}
}

func newFakeRemoteProvisioner() *fakeRemoteProvisioner {
	return &fakeRemoteProvisioner{
		dsn:            "postgres://postgres:targetpw@localhost:5440/postgres?sslmode=disable",
		marked:         make(chan string, 4),
		restoredSignal: make(chan struct{}, 4),
	}
}

func (f *fakeRemoteProvisioner) ProvisionInstance(_ context.Context, spec RemoteTargetSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provInstanceErr != nil {
		return "", f.provInstanceErr
	}
	f.provisionedKind = targetInstance
	f.provisionedSpec = spec
	return "new-inst-1", nil
}

func (f *fakeRemoteProvisioner) ProvisionCluster(_ context.Context, spec RemoteTargetSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provClusterErr != nil {
		return "", f.provClusterErr
	}
	f.provisionedKind = targetCluster
	f.provisionedSpec = spec
	return "new-clus-1", nil
}

func (f *fakeRemoteProvisioner) WaitReady(_ context.Context, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.waitErr != nil {
		return "", f.waitErr
	}
	return f.dsn, nil
}

func (f *fakeRemoteProvisioner) MarkError(_ context.Context, kind, id, reason string) {
	f.marked <- kind + ":" + id + ":" + reason
}

// validCaptureBody returns a JSON capture request body.
func validCaptureBody() string {
	return `{"host":"db.example.com","port":5432,"user":"alice","password":"s3cr3t-pw","dbname":"shop","sslmode":"require"}`
}

func newRemoteHandler(svc RemoteService, prov RemoteTargetProvisioner) *RemoteHandler {
	h := NewRemoteHandler(svc, prov)
	h.restoreTimeout = 5 * time.Second // keep background test work bounded
	return h
}

// --- Capture ---

func TestRemoteCaptureSuccess(t *testing.T) {
	svc := newFakeRemoteService()
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remote/backups", strings.NewReader(validCaptureBody()))
	h.Capture(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	// Password must never be echoed.
	if strings.Contains(rr.Body.String(), "s3cr3t-pw") {
		t.Fatalf("response leaked password: %s", rr.Body.String())
	}
	var resp struct {
		Backup remoteDumpPayload `json:"backup"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Backup.ID == "" {
		t.Fatalf("missing dump id")
	}
	if resp.Backup.SourceHost != "[REDACTED].com" {
		t.Fatalf("source host not redacted: %q", resp.Backup.SourceHost)
	}
	// The service received the full connection (including password) for the dump.
	if len(svc.captured) != 1 || svc.captured[0].Password != "s3cr3t-pw" {
		t.Fatalf("service did not receive the connection: %+v", svc.captured)
	}
}

func TestRemoteCaptureValidationRejectsMissingHost(t *testing.T) {
	svc := newFakeRemoteService()
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	rr := httptest.NewRecorder()
	body := `{"host":"","user":"alice","password":"pw","dbname":"shop"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remote/backups", strings.NewReader(body))
	h.Capture(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if len(svc.captured) != 0 {
		t.Fatalf("must not attempt capture on invalid input")
	}
}

func TestRemoteCaptureRejectsUnknownFields(t *testing.T) {
	h := newRemoteHandler(newFakeRemoteService(), newFakeRemoteProvisioner())
	rr := httptest.NewRecorder()
	body := `{"host":"h","user":"u","dbname":"d","password":"p","evil":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remote/backups", strings.NewReader(body))
	h.Capture(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown field", rr.Code)
	}
}

func TestRemoteCaptureErrorIsRedacted(t *testing.T) {
	svc := newFakeRemoteService()
	// Service returns an error that (already redacted) must not leak.
	svc.captureErr = apperr.New(apperr.KindInternal, "remotebackup: pg_dump failed: password=[REDACTED] auth")
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/remote/backups", strings.NewReader(validCaptureBody()))
	h.Capture(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	// 5xx bodies are generic, so the original message (and any secret) is gone.
	if strings.Contains(rr.Body.String(), "s3cr3t-pw") || strings.Contains(rr.Body.String(), "pg_dump") {
		t.Fatalf("5xx body leaked detail: %s", rr.Body.String())
	}
}

// --- List ---

func TestRemoteListReturnsEntries(t *testing.T) {
	svc := newFakeRemoteService()
	_, _ = svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/remote/backups", nil)
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp struct {
		Backups []remoteDumpPayload `json:"backups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Backups) != 1 {
		t.Fatalf("want 1 backup, got %d", len(resp.Backups))
	}
}

// --- Restore ---

// withURLParam returns a request whose chi route context carries {id}=id.
func withURLParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestRemoteRestoreInstanceSuccess(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	prov := newFakeRemoteProvisioner()
	h := newRemoteHandler(svc, prov)

	body := `{"target":"instance","name":"adopted","password":"longenough","repo_type":"local","pg_version":"16"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/api/v1/remote/backups/"+e.ID+"/restore", strings.NewReader(body)),
		"id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Target string `json:"target"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Target != "instance" || resp.ID != "new-inst-1" {
		t.Fatalf("unexpected target/id: %+v", resp)
	}

	// The background goroutine should wait-ready then restore into the DSN.
	waitForRestore(t, svc)
	if svc.restoredID != e.ID {
		t.Fatalf("restored wrong dump: %q", svc.restoredID)
	}
	if svc.restoredDSN != prov.dsn {
		t.Fatalf("restored into wrong dsn: %q", svc.restoredDSN)
	}
}

func TestRemoteRestoreClusterSuccess(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	prov := newFakeRemoteProvisioner()
	h := newRemoteHandler(svc, prov)

	body := `{"target":"cluster","name":"adopted","password":"longenough","replicas":2}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/api/v1/remote/backups/"+e.ID+"/restore", strings.NewReader(body)),
		"id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	waitForRestore(t, svc)
	if prov.provisionedKind != targetCluster {
		t.Fatalf("expected cluster provision, got %q", prov.provisionedKind)
	}
	if prov.provisionedSpec.Replicas != 2 {
		t.Fatalf("replicas not propagated: %d", prov.provisionedSpec.Replicas)
	}
}

func TestRemoteRestoreRejectsBadTarget(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	body := `{"target":"galaxy","name":"x","password":"longenough"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/"+e.ID+"/restore", strings.NewReader(body)), "id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for bad target", rr.Code)
	}
}

func TestRemoteRestoreRejectsShortPassword(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	body := `{"target":"instance","name":"x","password":"short"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/"+e.ID+"/restore", strings.NewReader(body)), "id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for short password", rr.Code)
	}
}

func TestRemoteRestoreClusterRequiresReplica(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	h := newRemoteHandler(svc, newFakeRemoteProvisioner())

	body := `{"target":"cluster","name":"x","password":"longenough","replicas":0}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/"+e.ID+"/restore", strings.NewReader(body)), "id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for cluster with 0 replicas", rr.Code)
	}
}

func TestRemoteRestoreUnknownDump404(t *testing.T) {
	h := newRemoteHandler(newFakeRemoteService(), newFakeRemoteProvisioner())

	body := `{"target":"instance","name":"x","password":"longenough"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/nope/restore", strings.NewReader(body)), "id", "nope")
	h.Restore(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown dump", rr.Code)
	}
}

func TestRemoteRestoreProvisionErrorSurfaced(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	prov := newFakeRemoteProvisioner()
	prov.provInstanceErr = apperr.New(apperr.KindConflict, "name already in use")
	h := newRemoteHandler(svc, prov)

	body := `{"target":"instance","name":"dup","password":"longenough"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/"+e.ID+"/restore", strings.NewReader(body)), "id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 from provision error", rr.Code)
	}
	if svc.restoreCalled {
		t.Fatalf("restore must not run when provisioning fails")
	}
}

func TestRemoteRestoreMarksTargetErroredOnRestoreFailure(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	svc.restoreErr = apperr.New(apperr.KindInternal, "remotebackup: pg_restore failed")
	prov := newFakeRemoteProvisioner()
	h := newRemoteHandler(svc, prov)

	body := `{"target":"instance","name":"x","password":"longenough"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/"+e.ID+"/restore", strings.NewReader(body)), "id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	// Background path must mark the target errored (not leave it half-built).
	select {
	case marked := <-prov.marked:
		if !strings.Contains(marked, "new-inst-1") || !strings.Contains(marked, "restore failed") {
			t.Fatalf("unexpected MarkError payload: %q", marked)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected the failed restore to MarkError the target")
	}
}

func TestRemoteRestoreMarksErroredWhenNeverReady(t *testing.T) {
	svc := newFakeRemoteService()
	e, _ := svc.Capture(context.Background(), remotebackup.RemoteConn{Host: "h", User: "u", DBName: "d", Password: "p"})
	prov := newFakeRemoteProvisioner()
	prov.waitErr = apperr.New(apperr.KindInternal, "provisioning timed out")
	h := newRemoteHandler(svc, prov)

	body := `{"target":"instance","name":"x","password":"longenough"}`
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/"+e.ID+"/restore", strings.NewReader(body)), "id", e.ID)
	h.Restore(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	select {
	case marked := <-prov.marked:
		if !strings.Contains(marked, "never became ready") {
			t.Fatalf("unexpected MarkError payload: %q", marked)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected MarkError when target never ready")
	}
	if svc.restoreCalled {
		t.Fatalf("restore must not run when target never ready")
	}
}

func TestRemoteRestoreNoProvisionerConfigured(t *testing.T) {
	h := NewRemoteHandler(newFakeRemoteService(), nil)
	rr := httptest.NewRecorder()
	req := withURLParam(
		httptest.NewRequest(http.MethodPost, "/x/d/restore", strings.NewReader(`{"target":"instance","name":"x","password":"longenough"}`)),
		"id", "d")
	h.Restore(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when provisioner not configured", rr.Code)
	}
}

// waitForRestore polls until the fake service records a restore call.
func waitForRestore(t *testing.T, svc *fakeRemoteService) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		svc.mu.Lock()
		done := svc.restoreCalled
		svc.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("restore was not invoked by the background goroutine")
}
