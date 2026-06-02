package ws

import (
	"testing"
	"time"
)

func TestSubscribeReceivesPublishedEvent(t *testing.T) {
	h := NewHub()
	sub, cancel := h.Subscribe()
	defer cancel()

	h.Publish("inst-1", "stanza", "creating")

	select {
	case ev := <-sub:
		if ev.InstanceID != "inst-1" || ev.Step != "stanza" || ev.Detail != "creating" {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive published event")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	sub, cancel := h.Subscribe()
	cancel()

	h.Publish("inst-1", "step", "detail")

	select {
	case _, ok := <-sub:
		if ok {
			t.Error("received event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// no delivery is acceptable too
	}
}

func TestPublishToMultipleSubscribers(t *testing.T) {
	h := NewHub()
	a, ca := h.Subscribe()
	b, cb := h.Subscribe()
	defer ca()
	defer cb()

	h.Publish("inst-1", "ready", "ok")

	for i, sub := range []<-chan Event{a, b} {
		select {
		case <-sub:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive event", i)
		}
	}
}

func TestPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe() // never drained
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range 1000 {
			h.Publish("inst", "step", "detail")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
