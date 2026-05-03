package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

// ---------------------------------------------------------------------------
// stub executor
// ---------------------------------------------------------------------------

type stubExecutor struct {
	tasks         []AssignedTask
	claimErr      error
	completeErr   error
	extendErr     error
	completeCalls []domaininstance.CompleteSpec
	extendCalled  int
}

func (s *stubExecutor) ClaimNextDispatched(_ context.Context, _, _ string, _ int, _, _ time.Time) ([]AssignedTask, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.tasks, nil
}

func (s *stubExecutor) CompleteInstance(_ context.Context, spec domaininstance.CompleteSpec) error {
	s.completeCalls = append(s.completeCalls, spec)
	return s.completeErr
}

func (s *stubExecutor) ExtendLease(_ context.Context, _ string, _ int64, _ string, _ time.Time) error {
	s.extendCalled++
	return s.extendErr
}

// ---------------------------------------------------------------------------
// stub handler
// ---------------------------------------------------------------------------

type stubHandler struct {
	result Result
}

func (h *stubHandler) Execute(_ context.Context, _ AssignedTask) Result {
	return h.result
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func sampleTask() AssignedTask {
	return AssignedTask{
		InstanceID:           1,
		RunID:                "run-abc",
		TenantID:             "default",
		JobID:                42,
		HandlerType:          "test",
		HandlerPayload:       map[string]any{},
		TimeoutSec:           10,
		Priority:             5,
		Attempt:              1,
		MaxAttempt:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: "fixed",
		ScheduledAt:          time.Now(),
		LeaseExpiresAt:       time.Now().Add(60 * time.Second),
	}
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestRunOnce_NoTasks(t *testing.T) {
	repo := &stubExecutor{}
	uc := NewTickUseCase(repo, nil)

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 0 {
		t.Fatalf("expected n=0, got %d", n)
	}
}

func TestRunOnce_FetchError(t *testing.T) {
	repo := &stubExecutor{claimErr: errors.New("db down")}
	uc := NewTickUseCase(repo, nil)

	_, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunOnce_ClaimError(t *testing.T) {
	repo := &stubExecutor{
		claimErr: errors.New("not claimed"),
	}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &stubHandler{}})

	_, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err == nil {
		t.Fatal("expected error when claim fails")
	}
}

func TestRunOnce_SuccessExecution(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(repo.completeCalls))
	}
	if repo.completeCalls[0].Status != domaininstance.StatusSuccess {
		t.Fatalf("expected status=success, got %q", repo.completeCalls[0].Status)
	}
}

func TestRunOnce_FailureWithRetry(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: false, ResultCode: "1", ErrorMsg: "boom"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	spec := repo.completeCalls[0]
	if spec.Status != domaininstance.StatusRetryWait {
		t.Fatalf("expected status=retry_wait (attempt 1 < max 3), got %q", spec.Status)
	}
	if spec.RetryAt == nil {
		t.Fatal("expected retry_at to be set")
	}
}

func TestRunOnce_FinalFailure(t *testing.T) {
	task := sampleTask()
	task.Attempt = 3
	task.MaxAttempt = 3
	repo := &stubExecutor{tasks: []AssignedTask{task}}
	handler := &stubHandler{result: Result{Success: false, ResultCode: "1", ErrorMsg: "final"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	if repo.completeCalls[0].Status != domaininstance.StatusFailed {
		t.Fatalf("expected status=failed, got %q", repo.completeCalls[0].Status)
	}
}

func TestRunOnce_UnknownHandler(t *testing.T) {
	task := sampleTask()
	task.HandlerType = "nonexistent"
	repo := &stubExecutor{tasks: []AssignedTask{task}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &stubHandler{}})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(repo.completeCalls))
	}
}

func TestRunOnce_CompleteError(t *testing.T) {
	repo := &stubExecutor{
		tasks:       []AssignedTask{sampleTask()},
		completeErr: errors.New("complete boom"),
	}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	if err == nil || !strings.Contains(err.Error(), "complete instance") {
		t.Fatalf("expected complete instance error, got %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
}
