package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// defaultNotifyTimeout bounds a webhook POST when none is configured.
const defaultNotifyTimeout = 5 * time.Second

// blockedDialControl rejects connections to link-local / cloud-metadata
// addresses (169.254.0.0/16, fe80::/10, and the IPv6 metadata fd00:ec2::254).
// It runs AFTER DNS resolution on the actual IP about to be dialed — including
// every redirect hop — so it defeats DNS-rebinding and redirect-to-metadata SSRF
// without breaking the legitimate self-hosted case of an internal/private
// webhook receiver (RFC1918 / loopback are intentionally still allowed).
func blockedDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("alerts: webhook host %q did not resolve to an IP", host)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || isMetadataIP(ip) {
		return fmt.Errorf("alerts: webhook target %s is a blocked link-local/metadata address", ip)
	}
	return nil
}

func isMetadataIP(ip net.IP) bool {
	// AWS/GCP/Azure IMDS and the IPv6 equivalent. Link-local already covers
	// 169.254.169.254, but the IPv6 metadata address is unique-local, so name it.
	return ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("fd00:ec2::254"))
}

// guardedHTTPClient returns an http.Client whose dialer refuses link-local /
// metadata targets on every hop.
func guardedHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: blockedDialControl}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
	}
}

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
	return &WebhookNotifier{url: url, client: guardedHTTPClient(timeout)}
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
