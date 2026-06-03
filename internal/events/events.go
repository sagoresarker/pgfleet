// Package events records a durable, append-only timeline of instance and
// cluster lifecycle, status, and health-transition events. It is the durable
// counterpart to the in-memory ws hub: events written here survive a
// control-plane restart and remain queryable.
package events

import (
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Event is a single persisted timeline entry.
type Event struct {
	ID         string
	InstanceID string // optional; empty if the event is not instance-scoped
	ClusterID  string // optional; empty if the event is not cluster-scoped
	Type       string // e.g. "provisioning", "status_change", "health_transition"
	Message    string
	Metadata   map[string]string // optional structured context
	CreatedAt  time.Time
}

// NewEvent is the input to Record: the caller-supplied fields of an Event.
type NewEvent struct {
	InstanceID string
	ClusterID  string
	Type       string
	Message    string
	Metadata   map[string]string
}

// Validate checks the required fields of a NewEvent.
func (e NewEvent) Validate() error {
	if strings.TrimSpace(e.Type) == "" {
		return apperr.New(apperr.KindInvalid, "events: type is required")
	}
	if strings.TrimSpace(e.Message) == "" {
		return apperr.New(apperr.KindInvalid, "events: message is required")
	}
	return nil
}
