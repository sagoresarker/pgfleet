package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookNotifierEmptyURLIsNoOp(t *testing.T) {
	n := NewWebhookNotifier("", 0)
	if err := n.Notify(context.Background(), []Transition{{Kind: KindDiskFull}}); err != nil {
		t.Fatalf("empty-URL notifier should be a no-op, got %v", err)
	}
}

func TestWebhookNotifierNoTransitionsIsNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, time.Second)
	if err := n.Notify(context.Background(), nil); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if called {
		t.Error("notifier should not POST when there are no transitions")
	}
}

func TestWebhookNotifierPostsJSONPayload(t *testing.T) {
	type payload struct {
		Transitions []struct {
			Kind       string    `json:"kind"`
			InstanceID string    `json:"instance_id"`
			State      string    `json:"state"`
			Message    string    `json:"message"`
			Severity   string    `json:"severity"`
			Timestamp  time.Time `json:"timestamp"`
		} `json:"transitions"`
	}

	var got payload
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ts := time.Now().UTC()
	n := NewWebhookNotifier(srv.URL, 2*time.Second)
	transitions := []Transition{
		{
			Kind:       KindDiskFull,
			InstanceID: "inst-1",
			From:       StateResolved,
			To:         StateFiring,
			State:      StateFiring,
			Severity:   SeverityCritical,
			Message:    "disk is full",
			Timestamp:  ts,
		},
	}
	if err := n.Notify(context.Background(), transitions); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if contentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", contentType)
	}
	if len(got.Transitions) != 1 {
		t.Fatalf("transitions len = %d, want 1", len(got.Transitions))
	}
	tr := got.Transitions[0]
	if tr.Kind != KindDiskFull || tr.InstanceID != "inst-1" || tr.State != StateFiring {
		t.Errorf("payload fields wrong: %+v", tr)
	}
	if tr.Severity != SeverityCritical || tr.Message != "disk is full" {
		t.Errorf("payload severity/message wrong: %+v", tr)
	}
	if tr.Timestamp.IsZero() {
		t.Error("timestamp should be serialized")
	}
}

func TestWebhookNotifierErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, time.Second)
	err := n.Notify(context.Background(), []Transition{{Kind: KindDiskFull, State: StateFiring}})
	if err == nil {
		t.Error("expected an error on a non-2xx webhook response")
	}
}

// TestWebhookNotifierBlocksMetadataEndpoint proves the SSRF guard refuses to
// POST to the cloud-metadata / link-local range (the classic SSRF escalation),
// failing fast at dial time rather than connecting.
func TestWebhookNotifierBlocksMetadataEndpoint(t *testing.T) {
	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://169.254.169.254:80/",
	} {
		n := NewWebhookNotifier(target, time.Second)
		err := n.Notify(context.Background(), []Transition{{Kind: KindDiskFull, Message: "x"}})
		if err == nil {
			t.Fatalf("%s: expected the metadata endpoint to be blocked, got nil", target)
		}
	}
}

// TestWebhookNotifierAllowsLoopback confirms the guard does NOT break the
// legitimate self-hosted case of an internal/loopback webhook receiver.
func TestWebhookNotifierAllowsLoopback(t *testing.T) {
	got := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notifyPayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		got <- len(p.Transitions)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, time.Second) // httptest binds 127.0.0.1 (loopback)
	if err := n.Notify(context.Background(), []Transition{{Kind: KindDiskFull, Message: "x"}}); err != nil {
		t.Fatalf("loopback webhook should be allowed, got %v", err)
	}
	if n := <-got; n != 1 {
		t.Fatalf("server saw %d transitions, want 1", n)
	}
}
