package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/health"
)

type fakeHealthStore struct{ reports []health.Report }

func (f fakeHealthStore) List(context.Context) ([]health.Report, error) { return f.reports, nil }

func TestHealthListReturnsReportsAndAlerts(t *testing.T) {
	store := fakeHealthStore{reports: []health.Report{
		{InstanceID: "i1", ArchivingOK: true, HasBackup: true, Issues: []string{}},
		{InstanceID: "i2", ArchivingOK: false, Issues: []string{"WAL archiving check is failing", "no backups exist"}},
	}}
	h := NewHealthHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Reports []map[string]any `json:"reports"`
		Alerts  []struct {
			InstanceID string `json:"instance_id"`
			Message    string `json:"message"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Reports) != 2 {
		t.Errorf("reports = %d, want 2", len(resp.Reports))
	}
	// Two issues on i2 become two alerts.
	if len(resp.Alerts) != 2 {
		t.Errorf("alerts = %d, want 2", len(resp.Alerts))
	}
	for _, a := range resp.Alerts {
		if a.InstanceID != "i2" {
			t.Errorf("alert for unexpected instance: %+v", a)
		}
	}
}
