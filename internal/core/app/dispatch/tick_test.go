package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type stubDispatcherRepo struct {
	calls                int
	found                []bool
	errAt                int
	recoverOrphansCalls  int
	recoverOrphansErr    error
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
	return domaininstance.Snapshot{}, s.found[i], nil
}

func (s *stubDispatcherRepo) RecoverLeaseOrphans(ctx context.Context, now time.Time) (int64, error) {
	s.recoverOrphansCalls++
	if s.recoverOrphansErr != nil {
		return 0, s.recoverOrphansErr
	}
	return 0, nil
}

func makeTestClaimSpec() domaininstance.ClaimSpec {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	return domaininstance.ClaimSpec{
		TenantID:       "tenant-test",
		WorkerID:       "worker-test",
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
