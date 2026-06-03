package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// manualTicker lets a test drive a job's interval deterministically.
func manualTicker(ch <-chan time.Time) TickerFunc {
	return func(time.Duration) (<-chan time.Time, func()) {
		return ch, func() {}
	}
}

func TestJobFiresOnEachTick(t *testing.T) {
	ticks := make(chan time.Time)
	ran := make(chan int, 4)

	s := New(WithTicker(manualTicker(ticks)))
	s.Register("count", time.Minute, func(context.Context) error {
		ran <- 1
		return nil
	})
	s.Start(context.Background())
	defer s.Stop()

	ticks <- time.Now()
	ticks <- time.Now()

	if a, b := <-ran, <-ran; a+b != 2 {
		t.Fatalf("expected job to run twice, got %d", a+b)
	}
}

func TestSchedulerSurvivesPanic(t *testing.T) {
	ticks := make(chan time.Time)
	ran := make(chan int32, 4)
	var calls int32

	s := New(WithTicker(manualTicker(ticks)))
	s.Register("panicky", time.Minute, func(context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		ran <- n
		if n == 1 {
			panic("boom")
		}
		return nil
	})
	s.Start(context.Background())
	defer s.Stop()

	ticks <- time.Now()
	if n := <-ran; n != 1 {
		t.Fatalf("first run = %d, want 1", n)
	}
	// If the panic killed the loop, this second tick would block forever.
	ticks <- time.Now()
	if n := <-ran; n != 2 {
		t.Fatalf("second run = %d, want 2 (scheduler did not survive panic)", n)
	}
}

func TestErrorHandlerInvokedOnJobError(t *testing.T) {
	ticks := make(chan time.Time)
	got := make(chan error, 1)
	wantErr := errors.New("backup failed")

	s := New(
		WithTicker(manualTicker(ticks)),
		WithErrorHandler(func(name string, err error) {
			if name == "failing" {
				got <- err
			}
		}),
	)
	s.Register("failing", time.Minute, func(context.Context) error {
		return wantErr
	})
	s.Start(context.Background())
	defer s.Stop()

	ticks <- time.Now()
	if err := <-got; !errors.Is(err, wantErr) {
		t.Fatalf("error handler got %v, want %v", err, wantErr)
	}
}

func TestStopHaltsJobs(t *testing.T) {
	ticks := make(chan time.Time)
	ran := make(chan struct{}, 1)
	var calls int32

	s := New(WithTicker(manualTicker(ticks)))
	s.Register("count", time.Minute, func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		ran <- struct{}{}
		return nil
	})
	s.Start(context.Background())

	ticks <- time.Now()
	<-ran

	s.Stop() // must return promptly and join the job goroutine

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls after stop = %d, want 1", got)
	}
}

// TestSchedulerRestartsAfterStop proves Stop() fully resets state so a later
// Start() actually relaunches the jobs (regression: Stop left s.cancel set, so
// the idempotent-Start guard turned every restart into a silent no-op, wedging
// backups/health/metrics/failover after any in-process Stop).
func TestSchedulerRestartsAfterStop(t *testing.T) {
	ticks := make(chan time.Time)
	ran := make(chan int, 4)

	s := New(WithTicker(manualTicker(ticks)))
	s.Register("count", time.Minute, func(context.Context) error {
		ran <- 1
		return nil
	})

	s.Start(context.Background())
	ticks <- time.Now()
	if <-ran != 1 {
		t.Fatal("job did not run before stop")
	}
	s.Stop()

	// Restart: the job must fire again. Before the fix this Start() was a no-op
	// and the tick below would block forever.
	s.Start(context.Background())
	defer s.Stop()
	ticks <- time.Now()
	select {
	case <-ran:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("job did not run after restart: Start() was a no-op")
	}
}
