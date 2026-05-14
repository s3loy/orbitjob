package execute

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// WorkerPool is a lightweight goroutine pool with capacity-based back-pressure.
// Submit is non-blocking: if the pool is at full capacity it returns false
// immediately, giving the caller a chance to back off rather than queue
// tasks indefinitely.
type WorkerPool struct {
	sem    chan struct{}
	active atomic.Int32
	wg     sync.WaitGroup
	closed atomic.Bool
}

// NewWorkerPool creates a pool that executes at most capacity tasks
// concurrently.  capacity must be >= 1.
func NewWorkerPool(capacity int) *WorkerPool {
	if capacity < 1 {
		capacity = 1
	}
	return &WorkerPool{
		sem: make(chan struct{}, capacity),
	}
}

// Submit tries to start fn in a new goroutine.  It returns true on success
// and false when the pool is already at capacity or has been stopped.
// The supplied context is passed to fn; cancellation is fn's responsibility.
// Panics inside fn are recovered so they do not crash the pool.
func (p *WorkerPool) Submit(ctx context.Context, fn func(ctx context.Context)) bool {
	if p.closed.Load() {
		return false
	}

	select {
	case p.sem <- struct{}{}:
		// Acquired a slot.
	default:
		return false
	}

	p.active.Add(1)
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()
		defer p.active.Add(-1)
		defer func() { <-p.sem }() // release slot
		defer func() {
			if r := recover(); r != nil {
				slog.Error("pool task panic recovered", "panic", r, "stack", string(debug.Stack()))
			}
		}()

		fn(ctx)
	}()

	return true
}

// Capacity returns the maximum number of concurrent tasks.
func (p *WorkerPool) Capacity() int {
	return cap(p.sem)
}

// Active returns the number of tasks currently running.
func (p *WorkerPool) Active() int {
	return int(p.active.Load())
}

// Wait blocks until all currently-running tasks finish.
func (p *WorkerPool) Wait() {
	p.wg.Wait()
}

// Stop prevents new submissions and waits until every in-flight task has
// finished.  After Stop returns, Submit will always return false.
func (p *WorkerPool) Stop() {
	p.closed.Store(true)
	p.wg.Wait()
}
