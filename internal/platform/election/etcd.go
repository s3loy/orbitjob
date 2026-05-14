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
	"orbitjob/internal/platform/metrics"
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

	// Create a reusable session for TryLock operations.
	// This avoids creating a new lease + keepalive for every lock acquisition.
	lockSession, err := concurrency.NewSession(cli, concurrency.WithTTL(int(cfg.TTL.Seconds())))
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("create lock session: %w", err)
	}

	coord := &etcdCoordinator{
		client:      cli,
		lockSession: lockSession,
		ttl:         cfg.TTL,
	}

	// Monitor lock session expiry; recreate on failure.
	go coord.monitorLockSession(cli, cfg.TTL)

	return coord, nil
}

type etcdCoordinator struct {
	client      *clientv3.Client
	lockSession *concurrency.Session
	ttl         time.Duration
	mu          sync.Mutex
}

// monitorLockSession watches the reusable session and recreates it if it expires.
func (e *etcdCoordinator) monitorLockSession(cli *clientv3.Client, ttl time.Duration) {
	for {
		<-e.lockSession.Done()
		metrics.EtcdSessionExpiresTotal.WithLabelValues("election").Inc()
		slog.Warn("etcd lock session expired, recreating")

		session, err := concurrency.NewSession(cli, concurrency.WithTTL(int(ttl.Seconds())))
		if err != nil {
			metrics.EtcdOperationErrorsTotal.WithLabelValues("session_recreate").Inc()
			slog.Error("failed to recreate etcd lock session", "error", err.Error())
			// Back off briefly before retrying.
			time.Sleep(time.Second)
			continue
		}

		e.mu.Lock()
		e.lockSession = session
		e.mu.Unlock()
		slog.Info("etcd lock session recreated")
	}
}

// Campaign blocks until this instance becomes leader for electionName.
// The returned context is cancelled when leadership is lost.
func (e *etcdCoordinator) Campaign(ctx context.Context, electionName string) (context.Context, error) {
	start := time.Now()

	session, err := concurrency.NewSession(e.client, concurrency.WithTTL(int(e.ttl.Seconds())))
	if err != nil {
		metrics.EtcdOperationErrorsTotal.WithLabelValues("campaign_session").Inc()
		return nil, fmt.Errorf("create session: %w", err)
	}

	elec := concurrency.NewElection(session, "/orbitjob/elections/"+electionName)

	// Campaign blocks until we become leader.
	if err := elec.Campaign(ctx, "instance"); err != nil {
		session.Close()
		metrics.EtcdOperationErrorsTotal.WithLabelValues("campaign").Inc()
		return nil, fmt.Errorf("campaign: %w", err)
	}

	metrics.EtcdCampaignDuration.Observe(time.Since(start).Seconds())
	slog.Info("became leader", "election", electionName)

	// Derive a context that is cancelled on session expiry or explicit resign.
	leaderCtx, cancel := context.WithCancel(context.Background())

	go func() {
		select {
		case <-session.Done():
			metrics.EtcdSessionExpiresTotal.WithLabelValues("campaign").Inc()
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
// Uses the reusable session to avoid lease creation overhead.
func (e *etcdCoordinator) TryLock(ctx context.Context, lockName string) (UnlockFunc, error) {
	start := time.Now()

	e.mu.Lock()
	session := e.lockSession
	e.mu.Unlock()

	select {
	case <-session.Done():
		// Session expired between check and use; let caller retry.
		metrics.EtcdOperationErrorsTotal.WithLabelValues("trylock_session_expired").Inc()
		return nil, ErrLocked
	default:
	}

	mutex := concurrency.NewMutex(session, "/orbitjob/locks/"+lockName)

	if err := mutex.TryLock(ctx); err != nil {
		if err == concurrency.ErrLocked {
			return nil, ErrLocked
		}
		metrics.EtcdOperationErrorsTotal.WithLabelValues("trylock").Inc()
		return nil, fmt.Errorf("try lock: %w", err)
	}

	metrics.EtcdLockAcquireDuration.WithLabelValues(lockName).Observe(time.Since(start).Seconds())

	return func() error {
		if err := mutex.Unlock(context.Background()); err != nil {
			return fmt.Errorf("unlock: %w", err)
		}
		return nil
	}, nil
}

func (e *etcdCoordinator) Close() error {
	e.lockSession.Close()
	return e.client.Close()
}
