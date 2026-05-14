package execute

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
	"orbitjob/internal/platform/metrics"
)

type executor interface {
	ClaimNextDispatched(ctx context.Context, tenantID, workerID string, limit int, leaseExpiresAt, now time.Time, labels map[string]any) ([]AssignedTask, error)
	CompleteInstance(ctx context.Context, spec domaininstance.CompleteSpec) error
	ExtendLease(ctx context.Context, tenantID string, instanceID int64, workerID string, newExpiry time.Time) error
}

type TickUseCase struct {
	repo         executor
	handlers     map[string]Handler
	dynamicLease *DynamicLease
}

func NewTickUseCase(repo executor, handlers map[string]Handler) *TickUseCase {
	return &TickUseCase{repo: repo, handlers: handlers}
}

// SetDynamicLease attaches a dynamic lease controller. When set, RunOnce
// records task execution durations to feed the lease EMA.
func (uc *TickUseCase) SetDynamicLease(dl *DynamicLease) {
	uc.dynamicLease = dl
}

func (uc *TickUseCase) RunOnce(
	ctx context.Context,
	tenantID, workerID string,
	limit int, leaseDuration time.Duration,
	labels map[string]any,
) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	leaseExpiresAt := now.Add(leaseDuration)

	tasks, err := uc.repo.ClaimNextDispatched(ctx, tenantID, workerID, limit, leaseExpiresAt, now, labels)
	if err != nil {
		return 0, fmt.Errorf("claim dispatched: %w", err)
	}
	if len(tasks) == 0 {
		return 0, nil
	}

	var wg sync.WaitGroup
	durations := make([]time.Duration, len(tasks))
	var mu sync.Mutex

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t AssignedTask) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("executeTask panic recovered", "panic", r, "stack", string(debug.Stack()))
				}
				wg.Done()
			}()
			d := uc.executeTask(ctx, tenantID, workerID, t, leaseDuration)
			mu.Lock()
			durations[idx] = d
			mu.Unlock()
		}(i, task)
	}

	wg.Wait()

	if uc.dynamicLease != nil {
		for _, d := range durations {
			if d > 0 {
				uc.dynamicLease.RecordExecution(d)
			}
		}
		metrics.WorkerLeaseDuration.WithLabelValues(workerID, tenantID).Set(uc.dynamicLease.Current().Seconds())
	}

	return len(tasks), nil
}

func (uc *TickUseCase) executeTask(
	ctx context.Context,
	tenantID, workerID string,
	task AssignedTask,
	leaseDuration time.Duration,
) time.Duration {
	start := time.Now()

	taskLog := slog.With("instance_id", task.InstanceID)
	if task.TraceID != nil {
		taskLog = taskLog.With("trace_id", *task.TraceID)
	}

	handler, ok := uc.handlers[task.HandlerType]
	if !ok {
		taskLog.Error("unknown handler type", "handler_type", task.HandlerType)
		metrics.ExecutionsTotal.WithLabelValues(task.HandlerType, "unknown_handler").Inc()
		uc.completeAsFailure(ctx, tenantID, task.InstanceID, workerID, task,
			"unknown_handler", fmt.Sprintf("no handler registered for type %q", task.HandlerType))
		return time.Since(start)
	}

	metrics.ExecutionsActive.Inc()
	defer metrics.ExecutionsActive.Dec()

	stopRenew := uc.startLeaseRenewal(ctx, tenantID, task.InstanceID, workerID, leaseDuration)
	defer stopRenew()

	timeoutDur := time.Duration(task.TimeoutSec) * time.Second
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, timeoutDur)
	var result Result
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("handler panic recovered", "panic", r, "stack", string(debug.Stack()))
				result = Result{
					Success:    false,
					ResultCode: "panic",
					ErrorMsg:   fmt.Sprintf("handler panic: %v", r),
				}
			}
		}()
		result = handler.Execute(timeoutCtx, task)
	}()
	cancelTimeout()

	metrics.ExecutionsTotal.WithLabelValues(task.HandlerType, result.ResultCode).Inc()

	completeSpec, err := domaininstance.NormalizeComplete(domaininstance.CompleteInput{
		TenantID:             tenantID,
		InstanceID:           task.InstanceID,
		WorkerID:             workerID,
		Success:              result.Success,
		ResultCode:           result.ResultCode,
		ErrorMsg:             result.ErrorMsg,
		Now:                  time.Now(),
		Attempt:              task.Attempt,
		MaxAttempt:           task.MaxAttempt,
		RetryBackoffSec:      task.RetryBackoffSec,
		RetryBackoffStrategy: task.RetryBackoffStrategy,
	})
	if err != nil {
		taskLog.Error("normalize complete failed", "error", err.Error())
		return time.Since(start)
	}

	// Use independent context for the DB write. The handler's ctx may already
	// be cancelled (shutdown), but we must persist the result — especially for
	// non-idempotent tasks where a lost write means a duplicate execution.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	if err := uc.repo.CompleteInstance(writeCtx, completeSpec); err != nil {
		taskLog.Error("complete instance failed", "error", err.Error())
	}

	return time.Since(start)
}

func (uc *TickUseCase) completeAsFailure(
	_ context.Context,
	tenantID string, instanceID int64, workerID string,
	task AssignedTask,
	resultCode, errorMsg string,
) {
	spec, err := domaininstance.NormalizeComplete(domaininstance.CompleteInput{
		TenantID:             tenantID,
		InstanceID:           instanceID,
		WorkerID:             workerID,
		Success:              false,
		ResultCode:           resultCode,
		ErrorMsg:             errorMsg,
		Now:                  time.Now(),
		Attempt:              task.Attempt,
		MaxAttempt:           task.MaxAttempt,
		RetryBackoffSec:      task.RetryBackoffSec,
		RetryBackoffStrategy: task.RetryBackoffStrategy,
	})
	if err != nil {
		return
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	if err := uc.repo.CompleteInstance(writeCtx, spec); err != nil {
		slog.Error("complete as failure failed",
			"instance_id", instanceID,
			"error", err.Error(),
		)
	}
}

func (uc *TickUseCase) startLeaseRenewal(
	ctx context.Context,
	tenantID string, instanceID int64, workerID string,
	leaseDuration time.Duration,
) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})

	interval := max(leaseDuration/3, time.Second)

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				// One final extension with a detached context so the running task
				// has time to finish before its lease expires and gets stolen.
				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				newExpiry := time.Now().Add(leaseDuration)
				if err := uc.repo.ExtendLease(writeCtx, tenantID, instanceID, workerID, newExpiry); err != nil {
						slog.Warn("final lease extension failed", "instance_id", instanceID, "error", err.Error())
					}
				cancel()
				return
			case <-ticker.C:
				newExpiry := time.Now().Add(leaseDuration)
				if err := uc.repo.ExtendLease(ctx, tenantID, instanceID, workerID, newExpiry); err != nil {
					metrics.LeaseExtensionFailuresTotal.Inc()
					slog.Warn("extend lease failed",
						"instance_id", instanceID,
						"error", err.Error(),
					)
				}
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}
