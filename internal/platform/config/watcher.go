package config

import "context"

// Watcher observes configuration changes from a backing store (etcd or env fallback).
type Watcher interface {
	// Watch starts watching a configuration key. The callback is invoked
	// whenever the value changes. Initial value is fetched immediately.
	// This method blocks until ctx is cancelled or an error occurs.
	Watch(ctx context.Context, key string, callback func(value string)) error

	// Close releases all resources.
	Close() error
}

// NopWatcher is a no-op Watcher for PG-only mode.
// Watch returns immediately when ctx is cancelled.
type NopWatcher struct{}

func (n *NopWatcher) Watch(ctx context.Context, _ string, _ func(string)) error {
	<-ctx.Done()
	return ctx.Err()
}

func (n *NopWatcher) Close() error { return nil }
