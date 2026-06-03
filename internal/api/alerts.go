package api

import (
	"context"
	"net/http"

	"github.com/sagoresarker/pgfleet/internal/alerts"
)

// alertLister is the read side of the alert store.
type alertLister interface {
	ListActive(ctx context.Context) ([]alerts.Alert, error)
	ListForInstance(ctx context.Context, instanceID string) ([]alerts.Alert, error)
}

// AlertsHandler serves the active-alerts view.
type AlertsHandler struct {
	store alertLister
}

// NewAlertsHandler builds an AlertsHandler.
func NewAlertsHandler(store alertLister) *AlertsHandler {
	return &AlertsHandler{store: store}
}

// List returns currently active alerts, optionally scoped to one instance via
// ?instance_id=.
func (h *AlertsHandler) List(w http.ResponseWriter, r *http.Request) {
	var (
		list []alerts.Alert
		err  error
	)
	if id := r.URL.Query().Get("instance_id"); id != "" {
		list, err = h.store.ListForInstance(r.Context(), id)
	} else {
		list, err = h.store.ListActive(r.Context())
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if list == nil {
		list = []alerts.Alert{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": list})
}
