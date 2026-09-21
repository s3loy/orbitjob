package config

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNopWatcherWatchReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := (&NopWatcher{}).Watch(ctx, "runtime.mode", func(string) { called = true })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Watch() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("the callback must never fire: PG-only mode has no values to watch")
	}
}

func TestNopWatcherWatchBlocksUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- (&NopWatcher{}).Watch(ctx, "runtime.mode", func(string) {})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Watch() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("NopWatcher.Watch returned before the context was cancelled")
	}
}

func TestNopWatcherCloseReturnsNil(t *testing.T) {
	if err := (&NopWatcher{}).Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}
