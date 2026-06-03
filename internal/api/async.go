package api

import (
	"context"
	"sync"
	"time"
)

// Async runs background work (provisioning, restores) launched from request
// handlers, tracked so a graceful shutdown can drain it before the process
// closes the DB pool and Docker runtime. Without this, a SIGTERM mid-provision
// would leave goroutines writing to a closed pool and instances/clusters
// wedged in "provisioning".
type Async struct {
	base   context.Context
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// NewAsync creates an Async bound to a base (server-lifetime) context.
func NewAsync(base context.Context) *Async {
	return &Async{base: base}
}

// Go runs fn in a tracked goroutine with the base context and panic recovery.
// A nil receiver runs fn in a plain goroutine with a background context (used
// when no tracker is wired, e.g. tests).
func (a *Async) Go(fn func(ctx context.Context)) {
	if a == nil {
		go func() {
			defer recoverAsync()
			fn(context.Background())
		}()
		return
	}
	// Guard Add with the same lock Wait uses to set closed, so Add can never
	// race with (or run after) the start of the shutdown drain — a
	// WaitGroup.Add-after-Wait misuse. Once closed, drop the new work: the
	// server has stopped accepting requests and the pool is about to close.
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.wg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.wg.Done()
		defer recoverAsync()
		fn(a.base)
	}()
}

// Wait blocks until all tracked work finishes or the timeout elapses. After
// Wait is called, further Go calls are dropped.
func (a *Async) Wait(timeout time.Duration) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func recoverAsync() { _ = recover() }
