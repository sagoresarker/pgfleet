package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/sagoresarker/pgfleet/internal/events"
)

// eventLister is the read side of the durable event store.
type eventLister interface {
	List(ctx context.Context, f events.Filter) ([]events.Event, error)
}

// EventsHistoryHandler serves the persisted (restart-surviving) event timeline.
type EventsHistoryHandler struct {
	store eventLister
}

// NewEventsHistoryHandler builds an EventsHistoryHandler.
func NewEventsHistoryHandler(store eventLister) *EventsHistoryHandler {
	return &EventsHistoryHandler{store: store}
}

type eventPayload struct {
	ID         string            `json:"id"`
	InstanceID string            `json:"instance_id,omitempty"`
	ClusterID  string            `json:"cluster_id,omitempty"`
	Type       string            `json:"type"`
	Message    string            `json:"message"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// List returns past events, filterable by ?instance_id=, ?cluster_id=, ?type=,
// and ?limit=.
func (h *EventsHistoryHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	evs, err := h.store.List(r.Context(), events.Filter{
		InstanceID: q.Get("instance_id"),
		ClusterID:  q.Get("cluster_id"),
		Type:       q.Get("type"),
		Limit:      limit,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	out := make([]eventPayload, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventPayload{
			ID: e.ID, InstanceID: e.InstanceID, ClusterID: e.ClusterID,
			Type: e.Type, Message: e.Message, Metadata: e.Metadata, CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
