package execute

import (
	"context"
	"fmt"
	"testing"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

// ---------------------------------------------------------------------------
// Mock repository
// ---------------------------------------------------------------------------

type benchExecutorRepo struct {
	tasks []AssignedTask
	err   error
}

func (m *benchExecutorRepo) ClaimNextDispatched(_ context.Context, _, _ string, _ int, _, _ time.Time) ([]AssignedTask, error) {
	return m.tasks, m.err
}

func (m *benchExecutorRepo) CompleteInstance(_ context.Context, _ domaininstance.CompleteSpec) error {
	return nil
}

func (m *benchExecutorRepo) ExtendLease(_ context.Context, _ string, _ int64, _ string, _ time.Time) error {
	return nil
}

// ---------------------------------------------------------------------------
// Mock handler
// ---------------------------------------------------------------------------

type benchHandler struct {
	result Result
}

func (h *benchHandler) Execute(_ context.Context, _ AssignedTask) Result {
	return h.result
}

type panicHandler struct{}

func (h *panicHandler) Execute(_ context.Context, _ AssignedTask) Result {
	panic("test panic")
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkExecuteTick(b *testing.B) {
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	task := AssignedTask{
		InstanceID: 1, RunID: "run-1", TenantID: "default", JobID: 42,
		HandlerType:          "http",
		HandlerPayload:       map[string]any{"url": "https://example.com/hook"},
		TimeoutSec:           30,
		Priority:             5,
		EffectivePriority:    5,
		Attempt:              1,
		MaxAttempt:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: "fixed",
		ScheduledAt:          now.Add(-time.Minute),
		DispatchedAt:         now.Add(-30 * time.Second),
	}

	handlers := map[string]Handler{"http": &benchHandler{result: Result{Success: true, ResultCode: "0"}}}

	tests := []struct {
		name string
		repo *benchExecutorRepo
	}{
		{"success", &benchExecutorRepo{tasks: []AssignedTask{task}}},
		{"no_tasks", &benchExecutorRepo{tasks: nil}},
		{"claim_error", &benchExecutorRepo{err: fmt.Errorf("db error")}},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				uc := NewTickUseCase(tt.repo, handlers)
				_, _ = uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
			}
		})
	}
}

func BenchmarkExecuteTick_PanicRecovery(b *testing.B) {
	task := AssignedTask{
		InstanceID:           1,
		RunID:                "run-1",
		TenantID:             "default",
		JobID:                42,
		HandlerType:          "panic",
		TimeoutSec:           30,
		Attempt:              1,
		MaxAttempt:           3,
		RetryBackoffSec:      10,
		RetryBackoffStrategy: "fixed",
	}

	handlers := map[string]Handler{"panic": &panicHandler{}}
	repo := &benchExecutorRepo{tasks: []AssignedTask{task}}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		uc := NewTickUseCase(repo, handlers)
		_, _ = uc.RunOnce(context.Background(), "default", "worker-1", 60*time.Second)
	}
}
