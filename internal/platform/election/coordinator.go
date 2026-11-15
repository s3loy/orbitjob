package election

import (
	"context"
	"errors"
	"time"
)

// EtcdConfig holds connection parameters for the etcd cluster.
// It is defined in the build-tag-free file so that both the etcd build
// and the stub build share the same type.
type EtcdConfig struct {
	Endpoints   []string
	DialTimeout time.Duration
	TTL         time.Duration // session TTL for leases
}

// Leadership owns cancelable leadership sessions. It does not promise fencing.
type Leadership interface {
	// Campaign blocks until this instance becomes leader for the given election.
	// The returned leaderCtx is cancelled when leadership is lost.
	Campaign(ctx context.Context, electionName string) (leaderCtx context.Context, err error)

	Close() error
}

// Coordinator adds a mutex contract supported by the etcd and memory backends.
// Kubernetes exposes Leadership only: an expiring mutex cannot safely report
// loss of authority through an UnlockFunc-only API.
type Coordinator interface {
	Leadership

	// TryLock attempts to acquire a distributed lock non-blocking.
	// Returns an unlock function that must be called to release the lock.
	TryLock(ctx context.Context, lockName string) (unlock UnlockFunc, err error)
}

// UnlockFunc releases a held lock. It is idempotent.
type UnlockFunc func() error

var (
	// ErrNotLeader means the caller is not the current leader.
	ErrNotLeader = errors.New("not the leader")

	// ErrSessionExpired means the underlying session/lease has expired.
	ErrSessionExpired = errors.New("election session expired")

	// ErrLocked means the lock is held by another session.
	ErrLocked = errors.New("lock is held by another session")
)
