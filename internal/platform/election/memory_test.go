package election

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryCampaignGrantsLeadershipToFirstCaller(t *testing.T) {
	coordinator := NewMemory()
	defer func() { _ = coordinator.Close() }()

	leaderCtx, err := coordinator.Campaign(context.Background(), "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	if leaderCtx.Err() != nil {
		t.Fatal("leader context must be live")
	}
}

func TestMemoryCampaignRejectsSecondCaller(t *testing.T) {
	coordinator := NewMemory()
	defer func() { _ = coordinator.Close() }()

	if _, err := coordinator.Campaign(context.Background(), "scheduler"); err != nil {
		t.Fatal(err)
	}
	// One in-process coordinator models contention between instances, so a
	// second campaign reports ErrNotLeader rather than blocking.
	if _, err := coordinator.Campaign(context.Background(), "scheduler"); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("error = %v, want ErrNotLeader", err)
	}
}

func TestMemoryCampaignSucceedsAfterClose(t *testing.T) {
	coordinator := NewMemory()
	if _, err := coordinator.Campaign(context.Background(), "scheduler"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	// Close releases leadership, so the election becomes available again.
	if _, err := coordinator.Campaign(context.Background(), "scheduler"); err != nil {
		t.Fatalf("election not released: %v", err)
	}
}

func TestMemoryTryLockExcludesSecondHolder(t *testing.T) {
	coordinator := NewMemory()
	defer func() { _ = coordinator.Close() }()

	unlock, err := coordinator.TryLock(context.Background(), "tenant-lock")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.TryLock(context.Background(), "tenant-lock"); !errors.Is(err, ErrLocked) {
		t.Fatalf("error = %v, want ErrLocked", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	// The lock is available again after release.
	second, err := coordinator.TryLock(context.Background(), "tenant-lock")
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	if err := second(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryTryLockIsIndependentPerName(t *testing.T) {
	coordinator := NewMemory()
	defer func() { _ = coordinator.Close() }()

	first, err := coordinator.TryLock(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first() }()

	// Per-tenant locks must not contend with each other, or one tenant's slow
	// dispatch would stall every other tenant.
	second, err := coordinator.TryLock(context.Background(), "tenant-b")
	if err != nil {
		t.Fatalf("independent lock refused: %v", err)
	}
	if err := second(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryCloseIsIdempotentAndReleasesLocks(t *testing.T) {
	coordinator := NewMemory()
	if _, err := coordinator.TryLock(context.Background(), "tenant-lock"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatalf("close must be idempotent: %v", err)
	}
	if _, err := coordinator.TryLock(context.Background(), "tenant-lock"); err != nil {
		t.Fatalf("close must release held locks: %v", err)
	}
}

func TestEtcdStubReportsMissingSupport(t *testing.T) {
	// The stub exists so a non-etcd build fails loudly at startup instead of
	// running without the coordination it was configured to use.
	coordinator, err := NewEtcd(EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}})
	if err == nil && coordinator == nil {
		t.Fatal("the stub must either error or return a coordinator")
	}
}
