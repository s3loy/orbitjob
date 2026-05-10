package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type stubDispatcherRepo struct {
	calls                   int
	found                   []bool
	errAt                   int
	recoverOrphansCalls     int
	recoverOrphansErr       error
	recoverOrphansDispatched int64
	recoverOrphansRunning   int64
	refreshPriorityErr      error
	recoverWorkersErr       error
	recoverWorkersResult    int64
	listTenantIDsResult     []string
	listTenantIDsErr        error
	snapTraceID             *string // non-nil when the snapshot should carry a TraceID
}

func (s *stubDispatcherRepo) DispatchOne(
	ctx context.Context,
	spec domaininstance.ClaimSpec,
	decide func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
) (domaininstance.Snapshot, bool, error) {
	i := s.calls
	s.calls++

	if s.errAt >= 0 && i == s.errAt {
		return domaininstance.Snapshot{}, false, errors.New("boom")
	}
	if i >= len(s.found) {
		return domaininstance.Snapshot{}, false, nil
	}
	return domaininstance.Snapshot{TraceID: s.snapTraceID}, s.found[i], nil
}

func (s *stubDispatcherRepo) RecoverLeaseOrphans(ctx context.Context, now time.Time) (int64, int64, error) {
	s.recoverOrphansCalls++
	if s.recoverOrphansErr != nil {
		return 0, 0, s.recoverOrphansErr
	}
	return s.recoverOrphansDispatched, s.recoverOrphansRunning, nil
}

func (s *stubDispatcherRepo) RecoverExpiredWorkers(ctx context.Context, now time.Time) (int64, error) {
	if s.recoverWorkersErr != nil {
		return 0, s.recoverWorkersErr
	}
	return s.recoverWorkersResult, nil
}

func (s *stubDispatcherRepo) RefreshEffectivePriority(ctx context.Context, now time.Time) (int64, error) {
	if s.refreshPriorityErr != nil {
		return 0, s.refreshPriorityErr
	}
	return 0, nil
}

func (s *stubDispatcherRepo) ListActiveTenantIDs(ctx context.Context) ([]string, error) {
	if s.listTenantIDsErr != nil {
		return nil, s.listTenantIDsErr
	}
	if s.listTenantIDsResult != nil {
		return s.listTenantIDsResult, nil
	}
	return []string{"default"}, nil
}

func makeTestClaimSpec() domaininstance.ClaimSpec {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	return domaininstance.ClaimSpec{
		TenantID:       "tenant-test",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
	}
}

func TestTickUseCase_RunBatch_StopsOnNoMoreCandidates(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, true, false}, errAt: -1}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("expected handled count=2, got %d", count)
	}
}

func TestTickUseCase_RunBatch_ReturnsPartialCountOnError(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, true, true}, errAt: 2}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 10)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if count != 2 {
		t.Fatalf("expected partial handled count=2, got %d", count)
	}
}

func TestTickUseCase_RunBatch_NormalizesLimit(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, false}, errAt: -1}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 0)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected handled count=1 when limit<=0, got %d", count)
	}
}

func TestTickUseCase_RunBatch_LimitReached(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, true, true}, errAt: -1}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 2)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("expected handled count=2, got %d", count)
	}
	if repo.calls != 2 {
		t.Fatalf("expected exactly 2 repo calls, got %d", repo.calls)
	}
}

func TestTickUseCase_RunBatch_RecoversOrphansBeforeDispatch(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, false}, errAt: -1}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	_, err := uc.RunBatch(context.Background(), spec, 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if repo.recoverOrphansCalls != 1 {
		t.Fatalf("expected 1 RecoverLeaseOrphans call, got %d", repo.recoverOrphansCalls)
	}
}

func TestTickUseCase_RunBatch_ReturnsErrorOnOrphanRecoveryFailure(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, false}, errAt: -1}
	repo.recoverOrphansErr = errors.New("recover boom")
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	_, err := uc.RunBatch(context.Background(), spec, 10)
	if err == nil || !errors.Is(err, repo.recoverOrphansErr) {
		t.Fatalf("expected orphan recovery error, got %v", err)
	}
}

func TestTickUseCase_RunBatch_ReturnsErrorOnRefreshPriorityFailure(t *testing.T) {
	repo := &stubDispatcherRepo{}
	repo.refreshPriorityErr = errors.New("refresh boom")
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	_, err := uc.RunBatch(context.Background(), spec, 10)
	if err == nil || !errors.Is(err, repo.refreshPriorityErr) {
		t.Fatalf("expected refresh priority error, got %v", err)
	}
}

func TestTickUseCase_RunBatch_ReturnsErrorOnRecoverExpiredWorkersFailure(t *testing.T) {
	repo := &stubDispatcherRepo{found: []bool{true, false}, errAt: -1}
	repo.recoverWorkersErr = errors.New("worker recovery boom")
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	_, err := uc.RunBatch(context.Background(), spec, 10)
	if err == nil || !errors.Is(err, repo.recoverWorkersErr) {
		t.Fatalf("expected recover expired workers error, got %v", err)
	}
}

func TestTickUseCase_ListActiveTenantIDs_Success(t *testing.T) {
	repo := &stubDispatcherRepo{listTenantIDsResult: []string{"tenant-a", "tenant-b"}}
	uc := NewTickUseCase(repo)
	ids, err := uc.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenantIDs() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 tenants, got %d", len(ids))
	}
	if ids[0] != "tenant-a" || ids[1] != "tenant-b" {
		t.Fatalf("expected [tenant-a tenant-b], got %v", ids)
	}
}

func TestTickUseCase_ListActiveTenantIDs_Error(t *testing.T) {
	repo := &stubDispatcherRepo{listTenantIDsErr: errors.New("db down")}
	uc := NewTickUseCase(repo)
	ids, err := uc.ListActiveTenantIDs(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ids != nil {
		t.Fatalf("expected nil ids on error, got %v", ids)
	}
}

func TestTickUseCase_ListActiveTenantIDs_Empty(t *testing.T) {
	repo := &stubDispatcherRepo{listTenantIDsResult: []string{}}
	uc := NewTickUseCase(repo)
	ids, err := uc.ListActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenantIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 tenants, got %d", len(ids))
	}
}

func TestTickUseCase_RunBatch_DispatchTraceID(t *testing.T) {
	tid := "trace-dispatch-1"
	repo := &stubDispatcherRepo{found: []bool{true, true, false}, errAt: -1, snapTraceID: &tid}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("expected handled count=2, got %d", count)
	}
}

func TestTickUseCase_RunBatch_OrphanRecoveryMetrics(t *testing.T) {
	repo := &stubDispatcherRepo{
		found:                    []bool{true, false},
		errAt:                    -1,
		recoverOrphansDispatched: 3,
		recoverOrphansRunning:    2,
	}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected handled count=1, got %d", count)
	}
	if repo.recoverOrphansCalls != 1 {
		t.Fatalf("expected 1 RecoverLeaseOrphans call, got %d", repo.recoverOrphansCalls)
	}
}

func TestTickUseCase_RunBatch_RecoveredWorkersMetrics(t *testing.T) {
	repo := &stubDispatcherRepo{
		found:                []bool{true, false},
		errAt:                -1,
		recoverWorkersResult: 1,
	}
	uc := NewTickUseCase(repo)
	spec := makeTestClaimSpec()
	count, err := uc.RunBatch(context.Background(), spec, 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected handled count=1, got %d", count)
	}
}
