package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestDoubleStartDoesNotLeak — calling Start twice must not orphan the first
// batch of goroutines. The old code overwrote s.cancel on the second Start, so
// Stop only cancelled the latest batch and wg.Wait() blocked forever.
func TestDoubleStartDoesNotLeak(t *testing.T) {
	ticks := make(chan time.Time)
	s := New(WithTicker(manualTicker(ticks)))
	s.Register("noop", time.Minute, func(context.Context) error { return nil })

	ctx := context.Background()
	s.Start(ctx)
	s.Start(ctx) // second Start must be a no-op

	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after a double Start (leaked goroutines)")
	}
}

// TestStopBeforeStartIsSafe — Stop on a never-started scheduler returns
// immediately without panicking on a nil cancel.
func TestStopBeforeStartIsSafe(t *testing.T) {
	s := New()
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop before Start blocked")
	}
}

// TestStopTwiceIsSafe — calling Stop twice must not panic or block.
func TestStopTwiceIsSafe(t *testing.T) {
	ticks := make(chan time.Time)
	s := New(WithTicker(manualTicker(ticks)))
	s.Register("noop", time.Minute, func(context.Context) error { return nil })
	s.Start(context.Background())

	s.Stop()
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second Stop blocked")
	}
}

// TestConcurrentRegisterIsRaceFree — concurrent Register calls before Start
// must not race on the jobs slice. Run with -race.
func TestConcurrentRegisterIsRaceFree(t *testing.T) {
	s := New(WithTicker(manualTicker(make(chan time.Time))))
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Register("j", time.Minute, func(context.Context) error { return nil })
		}()
	}
	wg.Wait()
	if len(s.jobs) != 50 {
		t.Fatalf("registered jobs = %d, want 50", len(s.jobs))
	}
}
