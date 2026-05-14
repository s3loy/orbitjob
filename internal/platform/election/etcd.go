//go:build etcd

package election

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// NewEtcd creates a production Coordinator backed by etcd.
func NewEtcd(cfg EtcdConfig) (Coordinator, error) {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.TTL == 0 {
		cfg.TTL = 15 * time.Second
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to etcd: %w", err)
	}

	return &etcdCoordinator{
		client: cli,
		ttl:    cfg.TTL,
	}, nil
}

type etcdCoordinator struct {
	client *clientv3.Client
	ttl    time.Duration
	mu     sync.Mutex
}

// Campaign blocks until this instance becomes leader for electionName.
// The returned context is cancelled when leadership is lost.
func (e *etcdCoordinator) Campaign(ctx context.Context, electionName string) (context.Context, error) {
	session, err := concurrency.NewSession(e.client, concurrency.WithTTL(int(e.ttl.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	elec := concurrency.NewElection(session, "/orbitjob/elections/"+electionName)

	// Campaign blocks until we become leader.
	if err := elec.Campaign(ctx, "instance"); err != nil {
		session.Close()
		return nil, fmt.Errorf("campaign: %w", err)
	}

	slog.Info("became leader", "election", electionName)

	// Derive a context that is cancelled on session expiry or explicit resign.
	leaderCtx, cancel := context.WithCancel(context.Background())

	go func() {
		select {
		case <-session.Done():
			slog.Warn("leadership lost", "election", electionName, "reason", "session_expired")
		case <-leaderCtx.Done():
			// Caller cancelled — resign gracefully.
			if err := elec.Resign(context.Background()); err != nil {
				slog.Error("resign failed", "election", electionName, "error", err)
			}
		}
		cancel()
		session.Close()
	}()

	return leaderCtx, nil
}

// TryLock attempts to acquire a distributed mutex non-blocking.
func (e *etcdCoordinator) TryLock(ctx context.Context, lockName string) (UnlockFunc, error) {
	session, err := concurrency.NewSession(e.client, concurrency.WithTTL(int(e.ttl.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	mutex := concurrency.NewMutex(session, "/orbitjob/locks/"+lockName)

	if err := mutex.TryLock(ctx); err != nil {
		session.Close()
		if err == concurrency.ErrLocked {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("try lock: %w", err)
	}

	return func() error {
		if err := mutex.Unlock(context.Background()); err != nil {
			return fmt.Errorf("unlock: %w", err)
		}
		session.Close()
		return nil
	}, nil
}

func (e *etcdCoordinator) Close() error {
	return e.client.Close()
}
