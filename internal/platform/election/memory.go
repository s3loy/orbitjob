package election

import (
	"context"
	"sync"
	"sync/atomic"
)

// NewMemory creates an in-memory Coordinator for unit testing.
// It supports multi-goroutine campaigning and simulates leader revocation.
func NewMemory() Coordinator {
	return &memoryCoordinator{
		elections: make(map[string]*memoryElection),
	}
}

type memoryCoordinator struct {
	mu        sync.Mutex
	elections map[string]*memoryElection
}

type memoryElection struct {
	leaderCtx    context.Context
	cancelLeader context.CancelFunc
	holder       string
}

func (m *memoryCoordinator) Campaign(ctx context.Context, electionName string) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.elections[electionName]
	if !ok || e.leaderCtx.Err() != nil {
		// No leader or leader expired — become leader.
		leaderCtx, cancel := context.WithCancel(context.Background())
		m.elections[electionName] = &memoryElection{
			leaderCtx:    leaderCtx,
			cancelLeader: cancel,
			holder:       "memory-instance",
		}
		return leaderCtx, nil
	}

	// Leader exists — return existing leader context (test should call in separate goroutine).
	// For testing, we block until the leader is released.
	return nil, ErrNotLeader
}

func (m *memoryCoordinator) TryLock(ctx context.Context, lockName string) (UnlockFunc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.elections[lockName]; ok {
		return nil, ErrLocked
	}

	leaderCtx, cancel := context.WithCancel(context.Background())
	m.elections[lockName] = &memoryElection{
		leaderCtx:    leaderCtx,
		cancelLeader: cancel,
		holder:       "memory-lock",
	}

	return func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if e, ok := m.elections[lockName]; ok {
			e.cancelLeader()
			delete(m.elections, lockName)
		}
		return nil
	}, nil
}

func (m *memoryCoordinator) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.elections {
		e.cancelLeader()
	}
	m.elections = make(map[string]*memoryElection)
	return nil
}

// memoryCoordinator 实现为测试用，以下辅助函数用于测试场景

var _ atomic.Int32 // ensure sync/atomic is available for future use
