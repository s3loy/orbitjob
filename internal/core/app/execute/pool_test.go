package execute

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_SubmitAndActive(t *testing.T) {
	pool := NewWorkerPool(2)

	var started atomic.Int32
	var done atomic.Int32
	block := make(chan struct{})

	// Submit first task — should succeed.
	if !pool.Submit(context.Background(), func(ctx context.Context) {
		started.Add(1)
		<-block
		done.Add(1)
	}) {
		t.Fatal("expected first submit to succeed")
	}

	// Submit second task — should succeed.
	if !pool.Submit(context.Background(), func(ctx context.Context) {
		started.Add(1)
		<-block
		done.Add(1)
	}) {
		t.Fatal("expected second submit to succeed")
	}

	// Wait for both to start.
	time.Sleep(50 * time.Millisecond)
	if started.Load() != 2 {
		t.Fatalf("expected 2 started, got %d", started.Load())
	}
	if pool.Active() != 2 {
		t.Fatalf("expected active=2, got %d", pool.Active())
	}

	// Third submit should be rejected (pool at capacity).
	if pool.Submit(context.Background(), func(ctx context.Context) {}) {
		t.Fatal("expected third submit to be rejected")
	}

	// Unblock tasks.
	close(block)
	pool.Wait()

	if done.Load() != 2 {
		t.Fatalf("expected 2 done, got %d", done.Load())
	}
	if pool.Active() != 0 {
		t.Fatalf("expected active=0 after wait, got %d", pool.Active())
	}
}

func TestWorkerPool_StopDrains(t *testing.T) {
	pool := NewWorkerPool(2)

	var done atomic.Int32
	block := make(chan struct{})

	for range 2 {
		pool.Submit(context.Background(), func(ctx context.Context) {
			<-block
			done.Add(1)
		})
	}

	// Stop should block until tasks finish.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(block)
	}()

	pool.Stop()
	if done.Load() != 2 {
		t.Fatalf("expected 2 done after stop, got %d", done.Load())
	}
}

func TestWorkerPool_PanicRecovery(t *testing.T) {
	pool := NewWorkerPool(2)

	var afterPanic atomic.Bool
	pool.Submit(context.Background(), func(ctx context.Context) {
		panic("intentional")
	})
	pool.Submit(context.Background(), func(ctx context.Context) {
		afterPanic.Store(true)
	})

	pool.Wait()

	if !afterPanic.Load() {
		t.Fatal("expected second task to run after first panicked")
	}
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	pool := NewWorkerPool(1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var receivedCtx context.Context
	pool.Submit(ctx, func(ctx context.Context) {
		receivedCtx = ctx
	})
	pool.Wait()

	if receivedCtx == nil {
		t.Fatal("expected task to receive context")
	}
	if receivedCtx.Err() == nil {
		t.Fatal("expected cancelled context to propagate")
	}
}

func TestWorkerPool_ConcurrentSubmit(t *testing.T) {
	pool := NewWorkerPool(5)

	var wg sync.WaitGroup
	var accepted atomic.Int32
	var rejected atomic.Int32

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if pool.Submit(context.Background(), func(ctx context.Context) {
				time.Sleep(10 * time.Millisecond)
			}) {
				accepted.Add(1)
			} else {
				rejected.Add(1)
			}
		}()
	}

	wg.Wait()
	pool.Wait()

	if accepted.Load()+rejected.Load() != 20 {
		t.Fatalf("expected accepted+rejected=20, got %d", accepted.Load()+rejected.Load())
	}
}

func TestWorkerPool_ZeroCapacityDefaultsToOne(t *testing.T) {
	pool := NewWorkerPool(0)

	if !pool.Submit(context.Background(), func(ctx context.Context) {}) {
		t.Fatal("expected submit to succeed with default capacity 1")
	}
	// Second should be rejected.
	if pool.Submit(context.Background(), func(ctx context.Context) {}) {
		t.Fatal("expected second submit to be rejected")
	}
}

func TestWorkerPool_StopRejectsNewSubmits(t *testing.T) {
	pool := NewWorkerPool(2)

	block := make(chan struct{})
	pool.Submit(context.Background(), func(ctx context.Context) {
		<-block
	})

	// Stop should wait for the running task.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(block)
	}()
	pool.Stop()

	// After Stop, new submits must be rejected.
	if pool.Submit(context.Background(), func(ctx context.Context) {}) {
		t.Fatal("expected submit to be rejected after Stop")
	}
}
