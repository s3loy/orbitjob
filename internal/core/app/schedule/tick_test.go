package schedule

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubSchedulerRepo struct {
	calls int
	found []bool
	errAt int
}

func (s *stubSchedulerRepo) ScheduleOneDueCron(
	ctx context.Context,
	now time.Time,
	decide func(time.Time, DueCronJob) (ScheduleDecision, error),
) (ScheduledOneResult, bool, error) {
	i := s.calls
	s.calls++

	if s.errAt >= 0 && i == s.errAt {
		return ScheduledOneResult{}, false, errors.New("boom")
	}
	if i >= len(s.found) {
		return ScheduledOneResult{}, false, nil
	}
	return ScheduledOneResult{}, s.found[i], nil
}

func TestTickUseCase_RunBatch_StopsOnNoMoreJobs(t *testing.T) {
	repo := &stubSchedulerRepo{found: []bool{true, true, false}, errAt: -1}
	uc := NewTickUseCase(repo)
	count, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("expected handled count=2, got %d", count)
	}
}

func TestTickUseCase_RunBatch_ReturnsPartialCountOnError(t *testing.T) {
	repo := &stubSchedulerRepo{found: []bool{true, true, true}, errAt: 2}
	uc := NewTickUseCase(repo)
	count, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if count != 2 {
		t.Fatalf("expected partial handled count=2, got %d", count)
	}
}

func TestTickUseCase_RunBatch_NormalizesLimit(t *testing.T) {
	repo := &stubSchedulerRepo{found: []bool{true, false}, errAt: -1}
	uc := NewTickUseCase(repo)
	count, err := uc.RunBatch(context.Background(), time.Now().UTC(), 0)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected handled count=1 when limit<=0, got %d", count)
	}
}
