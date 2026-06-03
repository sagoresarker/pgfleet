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
	base context.Context
	wg   sync.WaitGroup
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
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer recoverAsync()
		fn(a.base)
	}()
}

// Wait blocks until all tracked work finishes or the timeout elapses.
func (a *Async) Wait(timeout time.Duration) {
	if a == nil {
		return
	}
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func recoverAsync() { _ = recover() }
