package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHandlerStreamsEvents(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, func(string) error { return nil }))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=valid"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Give the server goroutine a moment to subscribe, then publish.
	time.Sleep(50 * time.Millisecond)
	hub.Publish("inst-1", "ready", "ok")

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var ev Event
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatalf("read: %v", err)
	}
	if ev.InstanceID != "inst-1" || ev.Step != "ready" {
		t.Errorf("event = %+v", ev)
	}
}

func TestHandlerRejectsBadToken(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, func(string) error { return http.ErrNoCookie }))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=bad"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatal("expected dial to fail for bad token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}
