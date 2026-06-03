package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncRunsAllAndWaits(t *testing.T) {
	a := NewAsync(context.Background())
	var n int32
	for range 100 {
		a.Go(func(context.Context) { atomic.AddInt32(&n, 1) })
	}
	a.Wait(2 * time.Second)
	if got := atomic.LoadInt32(&n); got != 100 {
		t.Fatalf("ran %d tasks, want 100", got)
	}
}

func TestAsyncWaitHonorsTimeout(t *testing.T) {
	a := NewAsync(context.Background())
	a.Go(func(context.Context) { time.Sleep(5 * time.Second) })
	start := time.Now()
	a.Wait(100 * time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("Wait blocked past its timeout on a slow task")
	}
}

func TestAsyncNilReceiverRunsAndWaitIsSafe(t *testing.T) {
	var a *Async
	done := make(chan struct{})
	a.Go(func(context.Context) { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nil-receiver Go did not run fn")
	}
	a.Wait(time.Second) // must not panic
}

func TestAsyncGoRecoversPanic(t *testing.T) {
	a := NewAsync(context.Background())
	a.Go(func(context.Context) { panic("boom") })
	a.Wait(time.Second) // must return; panic recovered, no crash
}

// TestAsyncGoAfterWaitIsDropped — once shutdown drain has begun (Wait called),
// new background work must be dropped, not tracked. Tracking it would be a
// WaitGroup.Add-after-Wait misuse (panic/race) and would write to a pool the
// shutdown is about to close.
func TestAsyncGoAfterWaitIsDropped(t *testing.T) {
	a := NewAsync(context.Background())
	a.Wait(time.Second) // begins/marks shutdown

	var ran int32
	a.Go(func(context.Context) { atomic.AddInt32(&ran, 1) })
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&ran) != 0 {
		t.Fatal("Go after Wait should be dropped during shutdown")
	}
}

// TestAsyncConcurrentGoAndWaitNoRace — concurrent Go and Wait must be race-free
// (run under -race).
func TestAsyncConcurrentGoAndWaitNoRace(t *testing.T) {
	a := NewAsync(context.Background())
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Go(func(context.Context) {})
		}()
	}
	go a.Wait(time.Second)
	wg.Wait()
	a.Wait(time.Second)
}
