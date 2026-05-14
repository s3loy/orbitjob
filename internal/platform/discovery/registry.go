package discovery

import (
	"context"
	"errors"
	"time"
)

// Registry abstracts service registration and discovery.
// Worker uses Register to announce itself; scheduler/dispatcher use
// ListInstances or WatchInstances to observe changes.
type Registry interface {
	// Register registers a service instance. ttl is the lease duration;
	// the caller must periodically invoke the returned KeepAliveFn.
	Register(ctx context.Context, serviceName, instanceID string, ttl time.Duration) (KeepAliveFn, error)

	// Deregister explicitly removes an instance (call on graceful shutdown).
	Deregister(ctx context.Context, serviceName, instanceID string) error

	// ListInstances returns currently alive instances for a service.
	ListInstances(ctx context.Context, serviceName string) ([]Instance, error)

	// WatchInstances subscribes to instance changes (add/remove).
	WatchInstances(ctx context.Context, serviceName string) (<-chan Event, error)

	// Close releases all resources.
	Close() error
}

// KeepAliveFn renews the registration lease. Call periodically (before TTL expires).
type KeepAliveFn func(ctx context.Context) error

// Instance describes a registered service member.
type Instance struct {
	ID      string
	Address string
	Labels  map[string]string
}

// Event represents a change in the instance set.
type Event struct {
	Type string // "put" or "delete"
	Instance
}

var (
	// ErrNotRegistered means the instance is not known to the registry.
	ErrNotRegistered = errors.New("instance not registered")

	// ErrSessionExpired means the underlying lease/session has expired.
	ErrSessionExpired = errors.New("registry session expired")
)
