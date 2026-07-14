package execute

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/platform/metrics"
)

// ---------------------------------------------------------------------------
// stub executor
// ---------------------------------------------------------------------------

type stubExecutor struct {
	mu            sync.Mutex
	tasks         []AssignedTask
	claimErr      error
	completeErr   error
	extendErr     error
	completeCalls []domaininstance.CompleteSpec
	extendCalled  int
}

func (s *stubExecutor) ClaimNextDispatched(_ context.Context, _, _ string, limit int, _, _ time.Time, _ map[string]any) ([]AssignedTask, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if limit > 0 && limit < len(s.tasks) {
		return s.tasks[:limit], nil
	}
	return s.tasks, nil
}

func (s *stubExecutor) CompleteInstance(_ context.Context, spec domaininstance.CompleteSpec) error {
	s.mu.Lock()
	s.completeCalls = append(s.completeCalls, spec)
	s.mu.Unlock()
	return s.completeErr
}

func (s *stubExecutor) ExtendLease(_ context.Context, _ string, _ int64, _ string, _ time.Time) error {
	s.mu.Lock()
	s.extendCalled++
	s.mu.Unlock()
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

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
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

	_, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunOnce_ClaimError(t *testing.T) {
	repo := &stubExecutor{
		claimErr: errors.New("not claimed"),
	}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &stubHandler{}})

	_, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
	if err == nil {
		t.Fatal("expected error when claim fails")
	}
}

func TestRunOnce_SuccessExecution(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
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

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
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

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
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

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
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

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	// CompleteInstance errors are logged, not returned — RunOnce still reports the task was handled
}

func TestRunOnce_ZeroLimit(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &stubHandler{}})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 0, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 0 {
		t.Fatalf("expected n=0 when limit=0, got %d", n)
	}
}

func TestRunOnce_NegativeLimit(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &stubHandler{}})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", -1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 0 {
		t.Fatalf("expected n=0 when limit<0, got %d", n)
	}
}

func TestRunOnce_LimitExceedsTaskCount(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 5, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1 when limit exceeds task count, got %d", n)
	}
}

func TestRunOnce_MultipleTasks(t *testing.T) {
	t1 := sampleTask()
	t1.InstanceID = 1
	t2 := sampleTask()
	t2.InstanceID = 2
	repo := &stubExecutor{tasks: []AssignedTask{t1, t2}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 3, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("expected n=2, got %d", n)
	}
	if len(repo.completeCalls) != 2 {
		t.Fatalf("expected 2 complete calls, got %d", len(repo.completeCalls))
	}
}

func TestExecuteTask_PanicRecovery(t *testing.T) {
	task := sampleTask()
	task.HandlerType = "panic"
	repo := &stubExecutor{tasks: []AssignedTask{task}}
	uc := NewTickUseCase(repo, map[string]Handler{"panic": &panicHandler{}})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call after panic recovery, got %d", len(repo.completeCalls))
	}
	if repo.completeCalls[0].ResultCode == nil || *repo.completeCalls[0].ResultCode != "panic" {
		t.Fatalf("expected result_code=panic, got %v", repo.completeCalls[0].ResultCode)
	}
}

// ---------------------------------------------------------------------------
// context-aware handler
// ---------------------------------------------------------------------------

type ctxCheckHandler struct {
	expectCancelled bool
	result          Result
}

func (h *ctxCheckHandler) Execute(ctx context.Context, _ AssignedTask) Result {
	if h.expectCancelled {
		select {
		case <-ctx.Done():
			return Result{Success: false, ResultCode: "context_cancelled", ErrorMsg: ctx.Err().Error()}
		default:
			panic("expected context to be cancelled")
		}
	}
	return h.result
}

func TestExecuteTask_ContextCancellation(t *testing.T) {
	// TimeoutSec=0 creates an immediately-cancelled context via WithTimeout(0).
	task := sampleTask()
	task.TimeoutSec = 0
	task.HandlerType = "ctx-check"
	repo := &stubExecutor{tasks: []AssignedTask{task}}
	uc := NewTickUseCase(repo, map[string]Handler{"ctx-check": &ctxCheckHandler{expectCancelled: true}})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// completeAsFailure tests
// ---------------------------------------------------------------------------

func TestCompleteAsFailure_NormalizeCompleteError(t *testing.T) {
	repo := &stubExecutor{}
	uc := NewTickUseCase(repo, nil)

	// InstanceID=0 triggers validation error in NormalizeComplete.
	task := sampleTask()
	uc.completeAsFailure(context.Background(), "default", 0, "", task, "code", "msg")

	// NormalizeComplete fails (instance_id < 1) → no CompleteInstance call.
	if len(repo.completeCalls) != 0 {
		t.Fatalf("expected 0 complete calls when NormalizeComplete fails, got %d", len(repo.completeCalls))
	}
}

func TestCompleteAsFailure_CompleteError(t *testing.T) {
	repo := &stubExecutor{completeErr: errors.New("complete failed")}
	uc := NewTickUseCase(repo, nil)

	task := sampleTask()
	uc.completeAsFailure(context.Background(), "default", task.InstanceID, "worker-1", task, "code", "msg")

	// CompleteInstance was called (error is logged, not returned).
	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(repo.completeCalls))
	}
}

// ---------------------------------------------------------------------------
// startLeaseRenewal tests
// ---------------------------------------------------------------------------

func TestExecuteTask_WithTraceID(t *testing.T) {
	tid := "trace-exec-1"
	task := sampleTask()
	task.TraceID = &tid
	repo := &stubExecutor{tasks: []AssignedTask{task}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
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

func TestExecuteTask_NormalizeCompleteError_EmptyWorkerID(t *testing.T) {
	// When workerID is empty, NormalizeComplete returns a validation error.
	// executeTask logs the error and returns without calling CompleteInstance.
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	// workerID="" triggers NormalizeComplete error
	n, err := uc.RunOnce(context.Background(), "default", "", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	// CompleteInstance must NOT be called because NormalizeComplete fails first.
	if len(repo.completeCalls) != 0 {
		t.Fatalf("expected 0 complete calls (NormalizeComplete fails with empty workerID), got %d", len(repo.completeCalls))
	}
}

func TestStartLeaseRenewal_Stop(t *testing.T) {
	repo := &stubExecutor{}
	uc := NewTickUseCase(repo, nil)

	ctx := context.Background()
	stop := uc.startLeaseRenewal(ctx, "default", 1, "worker-1", 1*time.Second)
	stop()

	// After stop, the goroutine is gone and no more ExtendLease calls happen.
	callsAfterStop := repo.extendCalled
	time.Sleep(50 * time.Millisecond)
	if repo.extendCalled != callsAfterStop {
		t.Fatal("extend called after stop — goroutine did not exit")
	}
}

func TestStartLeaseRenewal_ContextCancellation(t *testing.T) {
	repo := &stubExecutor{}
	uc := NewTickUseCase(repo, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before starting

	stop := uc.startLeaseRenewal(ctx, "default", 1, "worker-1", 1*time.Second)
	// Let goroutine react to context cancellation; do not call stop() because
	// done and ctx.Done() race — we want to test the ctx.Done() path.
	time.Sleep(50 * time.Millisecond)
	stop()

	// Context was already cancelled → goroutine still does one final ExtendLease
	// with a detached context before exiting.
	if repo.extendCalled != 1 {
		t.Fatalf("expected 1 final extend call after context cancellation, got %d", repo.extendCalled)
	}
}

func TestStartLeaseRenewal_ContextExpiresDuringLoop(t *testing.T) {
	// Start renewer with long interval (30s lease → 10s interval) and short context
	// timeout so that ctx.Done() fires before any ticker tick.
	repo := &stubExecutor{}
	uc := NewTickUseCase(repo, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	stop := uc.startLeaseRenewal(ctx, "default", 1, "worker-1", 30*time.Second)

	// Wait for context to expire and goroutine to react.
	time.Sleep(200 * time.Millisecond)
	stop()

	// Context should have expired before first ticker fire (interval = 10s).
	// ExtendLease may have been called 0-1 times depending on timing,
	// but the key is the goroutine exited cleanly.
}

func TestStartLeaseRenewal_TickerFires(t *testing.T) {
	repo := &stubExecutor{}
	uc := NewTickUseCase(repo, nil)

	// Interval = leaseDuration/3 but clamped to min 1s. Use 1s lease -> 1s interval.
	ctx := context.Background()
	stop := uc.startLeaseRenewal(ctx, "default", 1, "worker-1", 1*time.Second)

	// Wait for at least one ticker cycle (interval = 1s).
	time.Sleep(1100 * time.Millisecond)
	stop()

	// At least one ExtendLease should have been called via the ticker.
	if repo.extendCalled < 1 {
		t.Fatalf("expected >=1 extend calls from ticker, got %d", repo.extendCalled)
	}
}

func TestStartLeaseRenewal_ExtendLeaseError(t *testing.T) {
	repo := &stubExecutor{extendErr: errors.New("extend failed")}
	uc := NewTickUseCase(repo, nil)

	ctx := context.Background()
	stop := uc.startLeaseRenewal(ctx, "default", 1, "worker-1", 1*time.Second)

	// Wait for one ticker cycle. ExtendLease will fail and the error path
	// (metrics.LeaseExtensionFailuresTotal.Inc + slog.Warn) will be exercised.
	time.Sleep(1100 * time.Millisecond)
	stop()

	if repo.extendCalled < 1 {
		t.Fatalf("expected >=1 extend calls, got %d", repo.extendCalled)
	}
}

// ---------------------------------------------------------------------------
// SubmitNext tests (async pool)
// ---------------------------------------------------------------------------

func TestSubmitNext_WithPool(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	pool := NewWorkerPool(2)

	n, err := uc.SubmitNext(context.Background(), pool, "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}

	pool.Wait()

	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(repo.completeCalls))
	}
	if repo.completeCalls[0].Status != domaininstance.StatusSuccess {
		t.Fatalf("expected status=success, got %q", repo.completeCalls[0].Status)
	}
}

func TestSubmitNext_PoolFull(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &stubHandler{}})

	pool := NewWorkerPool(1)

	// Fill the pool.
	block := make(chan struct{})
	pool.Submit(context.Background(), func(ctx context.Context) {
		<-block
	})

	// Pool is full — SubmitNext should return 0 without claiming.
	n, err := uc.SubmitNext(context.Background(), pool, "default", "worker-1", 5, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 0 {
		t.Fatalf("expected n=0 when pool full, got %d", n)
	}
	if len(repo.completeCalls) != 0 {
		t.Fatalf("expected 0 complete calls (nothing claimed), got %d", len(repo.completeCalls))
	}

	close(block)
	pool.Wait()
}

func TestSubmitNext_PoolExecutesAsync(t *testing.T) {
	t1 := sampleTask()
	t1.InstanceID = 1
	t2 := sampleTask()
	t2.InstanceID = 2
	repo := &stubExecutor{tasks: []AssignedTask{t1, t2}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	pool := NewWorkerPool(2)

	n, err := uc.SubmitNext(context.Background(), pool, "default", "worker-1", 3, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("expected n=2, got %d", n)
	}

	// Return immediately — tasks are running asynchronously in pool.
	// Wait for them to finish.
	pool.Wait()

	if len(repo.completeCalls) != 2 {
		t.Fatalf("expected 2 complete calls, got %d", len(repo.completeCalls))
	}
}

func TestSubmitNext_NilPool(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	// nil pool = synchronous execution (backward compat).
	n, err := uc.SubmitNext(context.Background(), nil, "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}
	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call, got %d", len(repo.completeCalls))
	}
}

func TestSubmitNext_ClaimLimitRespected(t *testing.T) {
	// 3 tasks in repo, limit=2, pool capacity=5 → should only claim 2.
	t1 := sampleTask()
	t1.InstanceID = 1
	t2 := sampleTask()
	t2.InstanceID = 2
	t3 := sampleTask()
	t3.InstanceID = 3
	repo := &stubExecutor{tasks: []AssignedTask{t1, t2, t3}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	pool := NewWorkerPool(5)

	n, err := uc.SubmitNext(context.Background(), pool, "default", "worker-1", 2, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("expected n=2 (limit=2), got %d", n)
	}

	pool.Wait()

	if len(repo.completeCalls) != 2 {
		t.Fatalf("expected 2 complete calls, got %d", len(repo.completeCalls))
	}
}

type slowStubHandler struct{ delay time.Duration }

func (h *slowStubHandler) Execute(_ context.Context, _ AssignedTask) Result {
	time.Sleep(h.delay)
	return Result{Success: true, ResultCode: "0"}
}

func TestSubmitNext_DynamicLeaseRecorded(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": &slowStubHandler{delay: 50 * time.Millisecond}})
	dl := NewDynamicLease(10*time.Second, 5*time.Minute, 0.1)
	uc.SetDynamicLease(dl)

	pool := NewWorkerPool(2)

	_, _ = uc.SubmitNext(context.Background(), pool, "default", "worker-1", 1, 60*time.Second, nil)
	pool.Wait()

	if dl.emaDuration == 0 {
		t.Fatal("expected dynamic lease EMA to be recorded")
	}
}

// TestSubmitNext_PoolFullFallback covers the race where the pool becomes full
// between claiming tasks and submitting them. When Submit returns false the
// task is executed inline so the already-taken lease is not wasted.
func TestSubmitNext_PoolFullFallback(t *testing.T) {
	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	pool := NewWorkerPool(1)
	pool.Stop() // closed=true but active=0 → available computation sees a free slot

	n, err := uc.SubmitNext(context.Background(), pool, "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("SubmitNext() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}

	// Task was executed inline because pool.Submit returned false (closed).
	if len(repo.completeCalls) != 1 {
		t.Fatalf("expected 1 complete call (inline fallback), got %d", len(repo.completeCalls))
	}
	if repo.completeCalls[0].Status != domaininstance.StatusSuccess {
		t.Fatalf("expected status=success, got %q", repo.completeCalls[0].Status)
	}
}

func TestExecuteTask_HandlerExecutionDurationMetricObserved(t *testing.T) {
	metrics.HandlerExecutionDuration.Reset()

	repo := &stubExecutor{tasks: []AssignedTask{sampleTask()}}
	handler := &stubHandler{result: Result{Success: true, ResultCode: "0"}}
	uc := NewTickUseCase(repo, map[string]Handler{"test": handler})

	n, err := uc.RunOnce(context.Background(), "default", "worker-1", 1, 60*time.Second, nil)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("expected n=1, got %d", n)
	}

	count := testutil.CollectAndCount(metrics.HandlerExecutionDuration, "orbitjob_handler_execution_duration_seconds")
	if count != 1 {
		t.Fatalf("expected 1 handler duration observation, got %d", count)
	}
}
