// Package scheduler runs named jobs on fixed intervals. It is the single
// periodic-execution primitive reused across PgFleet for scheduled backups,
// pgBackRest health checks, metrics polling, retention/expire, and restore
// drills.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// JobFunc is the work performed on each tick. Returning an error invokes the
// scheduler's error handler; a panic is recovered and treated as an error.
type JobFunc func(ctx context.Context) error

// TickerFunc produces a tick channel for the given interval plus a stop func.
// It is injectable so tests can drive intervals deterministically.
type TickerFunc func(d time.Duration) (<-chan time.Time, func())

type job struct {
	name     string
	interval time.Duration
	run      JobFunc
}

// Scheduler runs registered jobs, each in its own goroutine.
type Scheduler struct {
	newTicker TickerFunc
	onError   func(name string, err error)

	mu     sync.Mutex
	jobs   []job
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Option configures a Scheduler.
type Option func(*Scheduler)

// WithTicker overrides the ticker factory (used in tests).
func WithTicker(tf TickerFunc) Option {
	return func(s *Scheduler) { s.newTicker = tf }
}

// WithErrorHandler sets a callback invoked when a job returns or panics.
func WithErrorHandler(h func(name string, err error)) Option {
	return func(s *Scheduler) { s.onError = h }
}

// New creates a Scheduler. By default it uses real time.Ticker and a no-op
// error handler.
func New(opts ...Option) *Scheduler {
	s := &Scheduler{
		newTicker: func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		},
		onError: func(string, error) {},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register adds a job. Must be called before Start.
func (s *Scheduler) Register(name string, interval time.Duration, fn JobFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job{name: name, interval: interval, run: fn})
}

// Start launches a goroutine per registered job. It returns immediately and is
// idempotent: a second Start while already running is a no-op, so it cannot
// orphan the first batch's cancel func (which would wedge Stop forever).
func (s *Scheduler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		cancel() // discard the unused child context
		return
	}
	s.cancel = cancel
	jobs := s.jobs
	s.mu.Unlock()

	for _, j := range jobs {
		s.wg.Add(1)
		go s.loop(ctx, j)
	}
}

// Stop cancels all jobs and waits for their goroutines to exit. It clears the
// stored cancel func AFTER the goroutines drain so a subsequent Start() relaunches
// the jobs instead of hitting the idempotent-Start guard and silently no-opping.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
}

func (s *Scheduler) loop(ctx context.Context, j job) {
	defer s.wg.Done()
	ch, stop := s.newTicker(j.interval)
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := s.runOnce(ctx, j); err != nil {
				s.onError(j.name, err)
			}
		}
	}
}

// runOnce executes a job, converting panics into errors so one bad run never
// kills the scheduler.
func (s *Scheduler) runOnce(ctx context.Context, j job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job %q panicked: %v", j.name, r)
		}
	}()
	return j.run(ctx)
}
