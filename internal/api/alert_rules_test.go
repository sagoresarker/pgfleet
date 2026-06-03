package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/alerts"
	"github.com/sagoresarker/pgfleet/internal/apperr"
)

type fakeRuleStore struct {
	mu    sync.Mutex
	items map[string]alerts.Rule
	seq   int
}

func newFakeRuleStore() *fakeRuleStore {
	return &fakeRuleStore{items: map[string]alerts.Rule{}}
}

func (f *fakeRuleStore) Create(_ context.Context, r alerts.Rule) (alerts.Rule, error) {
	if err := r.Validate(); err != nil {
		return alerts.Rule{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	r.ID = "rule-" + string(rune('0'+f.seq))
	f.items[r.ID] = r
	return r, nil
}

func (f *fakeRuleStore) List(_ context.Context) ([]alerts.Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]alerts.Rule, 0, len(f.items))
	for _, r := range f.items {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRuleStore) Update(_ context.Context, r alerts.Rule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[r.ID]; !ok {
		return apperr.New(apperr.KindNotFound, "not found")
	}
	f.items[r.ID] = r
	return nil
}

func (f *fakeRuleStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return apperr.New(apperr.KindNotFound, "not found")
	}
	delete(f.items, id)
	return nil
}

func mountAlertRules(store RuleStore) http.Handler {
	h := NewAlertRulesHandler(store)
	r := chi.NewRouter()
	r.Post("/api/v1/alert-rules", h.Create)
	r.Get("/api/v1/alert-rules", h.List)
	r.Put("/api/v1/alert-rules/{id}", h.Update)
	r.Delete("/api/v1/alert-rules/{id}", h.Delete)
	return r
}

func TestAlertRuleCreateOK(t *testing.T) {
	store := newFakeRuleStore()
	h := mountAlertRules(store)

	rr := postJSON(t, h, "/api/v1/alert-rules",
		`{"kind":"disk_full","threshold":15,"severity":"warning","enabled":true}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Rule alerts.Rule `json:"rule"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Rule.ID == "" || resp.Rule.Kind != "disk_full" {
		t.Errorf("unexpected rule payload: %+v", resp.Rule)
	}
}

func TestAlertRuleCreateValidation(t *testing.T) {
	h := mountAlertRules(newFakeRuleStore())
	for _, body := range []string{
		`{"kind":"cpu","threshold":15,"severity":"warning","enabled":true}`,    // bad kind
		`{"kind":"disk_full","threshold":15,"severity":"info","enabled":true}`, // bad severity
		`{"bogus_field":1}`, // unknown field rejected by strict decode
		`not json`,
	} {
		rr := postJSON(t, h, "/api/v1/alert-rules", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400", body, rr.Code)
		}
	}
}

func TestAlertRuleList(t *testing.T) {
	store := newFakeRuleStore()
	_, _ = store.Create(context.Background(), alerts.Rule{Kind: alerts.KindDiskFull, Severity: alerts.SeverityWarning, Threshold: 10, Enabled: true})
	h := mountAlertRules(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Rules []alerts.Rule `json:"rules"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Rules) != 1 {
		t.Errorf("rules len = %d, want 1", len(resp.Rules))
	}
}

func TestAlertRuleUpdate(t *testing.T) {
	store := newFakeRuleStore()
	r, _ := store.Create(context.Background(), alerts.Rule{Kind: alerts.KindDiskFull, Severity: alerts.SeverityWarning, Threshold: 10, Enabled: true})
	h := mountAlertRules(store)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/"+r.ID,
		strings.NewReader(`{"kind":"disk_full","threshold":20,"severity":"critical","enabled":false}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	got := store.items[r.ID]
	if got.Threshold != 20 || got.Severity != "critical" || got.Enabled {
		t.Errorf("update not applied: %+v", got)
	}
}

func TestAlertRuleUpdateMissingIs404(t *testing.T) {
	h := mountAlertRules(newFakeRuleStore())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/ghost",
		strings.NewReader(`{"kind":"disk_full","threshold":20,"severity":"critical","enabled":false}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestAlertRuleDeleteReturns204(t *testing.T) {
	store := newFakeRuleStore()
	r, _ := store.Create(context.Background(), alerts.Rule{Kind: alerts.KindDiskFull, Severity: alerts.SeverityWarning, Threshold: 10, Enabled: true})
	h := mountAlertRules(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules/"+r.ID, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if _, ok := store.items[r.ID]; ok {
		t.Errorf("rule was not deleted")
	}
}

func TestAlertRuleDeleteMissingIs404(t *testing.T) {
	h := mountAlertRules(newFakeRuleStore())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules/ghost", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}
