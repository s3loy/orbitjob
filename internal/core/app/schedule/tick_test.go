package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "orbitjob/internal/core/domain"
)

type classifiedStubRepo struct {
	calls   int
	results []classifiedResult
}

type classifiedResult struct {
	result ScheduledOneResult
	found  bool
	err    error
}

func (s *classifiedStubRepo) ScheduleOneDueCron(
	ctx context.Context,
	now time.Time,
	decide func(time.Time, DueCronJob) (ScheduleDecision, error),
) (ScheduledOneResult, bool, error) {
	if s.calls >= len(s.results) {
		return ScheduledOneResult{}, false, nil
	}
	r := s.results[s.calls]
	s.calls++
	return r.result, r.found, r.err
}

func testClassify(err error) domain.ErrorClass {
	if err == nil {
		return domain.ClassNone
	}
	switch err.Error() {
	case "fatal":
		return domain.FatalWorthy
	case "backoff":
		return domain.BackoffWorthy
	case "skip":
		return domain.SkipWorthy
	}
	return domain.BackoffWorthy
}

func TestRunBatch_NormalFlow(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, result: ScheduledOneResult{TraceID: "t1"}},
		{found: true, result: ScheduledOneResult{TraceID: "t2"}},
		{found: false},
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 2 {
		t.Fatalf("expected handled=2, got %d", counts.Handled)
	}
	if counts.Skipped != 0 || counts.Backoff != 0 || counts.Fatal != 0 {
		t.Fatalf("expected no errors, got skip=%d backoff=%d fatal=%d", counts.Skipped, counts.Backoff, counts.Fatal)
	}
}

func TestRunBatch_StopsOnNoMoreJobs(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, result: ScheduledOneResult{}},
		{found: true, result: ScheduledOneResult{}},
		{found: false},
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 2 {
		t.Fatalf("expected handled=2, got %d", counts.Handled)
	}
}

func TestRunBatch_FatalTerminates(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, result: ScheduledOneResult{TraceID: "t1"}},
		{found: true, err: errors.New("fatal")},
		{found: true, result: ScheduledOneResult{TraceID: "t3"}}, // never reached
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 2 {
		t.Fatalf("expected handled=2 (stopped at fatal), got %d", counts.Handled)
	}
	if counts.Fatal != 1 {
		t.Fatalf("expected fatal=1, got %d", counts.Fatal)
	}
}

func TestRunBatch_BackoffContinues(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, err: errors.New("backoff")},
		{found: true, result: ScheduledOneResult{TraceID: "t2"}},
		{found: false},
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 2 {
		t.Fatalf("expected handled=2, got %d", counts.Handled)
	}
	if counts.Backoff != 1 {
		t.Fatalf("expected backoff=1, got %d", counts.Backoff)
	}
}

func TestRunBatch_SkipContinues(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, err: errors.New("skip")},
		{found: true, result: ScheduledOneResult{TraceID: "t2"}},
		{found: false},
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 2 {
		t.Fatalf("expected handled=2, got %d", counts.Handled)
	}
	if counts.Skipped != 1 {
		t.Fatalf("expected skipped=1, got %d", counts.Skipped)
	}
}

func TestRunBatch_NormalizesLimit(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, result: ScheduledOneResult{}},
		{found: false},
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 0)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 1 {
		t.Fatalf("expected handled=1 when limit=0, got %d", counts.Handled)
	}
}

func TestRunBatch_TraceID(t *testing.T) {
	repo := &classifiedStubRepo{results: []classifiedResult{
		{found: true, result: ScheduledOneResult{TraceID: "trace-abc"}},
		{found: true, result: ScheduledOneResult{TraceID: "trace-abc"}},
		{found: false},
	}}
	uc := NewTickUseCase(repo, testClassify)
	counts, err := uc.RunBatch(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("RunBatch() error = %v", err)
	}
	if counts.Handled != 2 {
		t.Fatalf("expected handled=2, got %d", counts.Handled)
	}
}
