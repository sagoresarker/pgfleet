package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// defaultNotifyTimeout bounds a webhook POST when none is configured.
const defaultNotifyTimeout = 5 * time.Second

// WebhookNotifier POSTs alert transitions to a configured URL as JSON. An empty
// URL makes Notify a no-op so notifications can be disabled by configuration.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier builds a WebhookNotifier. A zero timeout uses a default.
func NewWebhookNotifier(url string, timeout time.Duration) *WebhookNotifier {
	if timeout <= 0 {
		timeout = defaultNotifyTimeout
	}
	return &WebhookNotifier{url: url, client: &http.Client{Timeout: timeout}}
}

// notifyTransition is the wire shape of a single transition in the payload.
type notifyTransition struct {
	Kind       string    `json:"kind"`
	InstanceID string    `json:"instance_id"`
	State      string    `json:"state"`
	Message    string    `json:"message"`
	Severity   string    `json:"severity"`
	Timestamp  time.Time `json:"timestamp"`
}

type notifyPayload struct {
	Transitions []notifyTransition `json:"transitions"`
}

// Notify POSTs the given transitions. It is a no-op when the URL is empty or
// there are no transitions to report.
func (n *WebhookNotifier) Notify(ctx context.Context, transitions []Transition) error {
	if n.url == "" || len(transitions) == 0 {
		return nil
	}

	payload := notifyPayload{Transitions: make([]notifyTransition, len(transitions))}
	for i, t := range transitions {
		payload.Transitions[i] = notifyTransition{
			Kind:       t.Kind,
			InstanceID: t.InstanceID,
			State:      t.To,
			Message:    t.Message,
			Severity:   t.Severity,
			Timestamp:  t.Timestamp,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "alerts: marshal notification", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "alerts: build notification request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "alerts: post notification", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apperr.New(apperr.KindInternal,
			fmt.Sprintf("alerts: notification webhook returned %d", resp.StatusCode))
	}
	return nil
}
