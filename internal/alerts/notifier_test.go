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
