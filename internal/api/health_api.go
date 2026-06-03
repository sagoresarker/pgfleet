package api

import (
	"context"
	"net/http"

	"github.com/sagoresarker/pgfleet/internal/health"
)

// HealthLister returns stored health reports.
type HealthLister interface {
	List(ctx context.Context) ([]health.Report, error)
}

// HealthHandler serves the fleet health and alerts view.
type HealthHandler struct {
	store HealthLister
}

// NewHealthHandler builds a HealthHandler.
func NewHealthHandler(store HealthLister) *HealthHandler {
	return &HealthHandler{store: store}
}

type alert struct {
	InstanceID string `json:"instance_id"`
	Message    string `json:"message"`
}

// List returns all health reports plus a flat list of active alerts (one per
// outstanding issue).
func (h *HealthHandler) List(w http.ResponseWriter, r *http.Request) {
	reports, err := h.store.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	alerts := make([]alert, 0)
	for _, rep := range reports {
		for _, issue := range rep.Issues {
			alerts = append(alerts, alert{InstanceID: rep.InstanceID, Message: issue})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": orEmpty(reports), "alerts": alerts})
}
