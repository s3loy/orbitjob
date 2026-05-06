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
	ClaimNextDispatched(ctx context.Context, tenantID, workerID string, limit int, leaseExpiresAt, now time.Time) ([]AssignedTask, error)
	CompleteInstance(ctx context.Context, spec domaininstance.CompleteSpec) error
	ExtendLease(ctx context.Context, tenantID string, instanceID int64, workerID string, newExpiry time.Time) error
}

type TickUseCase struct {
	repo     executor
	handlers map[string]Handler
}

func NewTickUseCase(repo executor, handlers map[string]Handler) *TickUseCase {
	return &TickUseCase{repo: repo, handlers: handlers}
}

func (uc *TickUseCase) RunOnce(
	ctx context.Context,
	tenantID, workerID string,
	limit int, leaseDuration time.Duration,
) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	leaseExpiresAt := now.Add(leaseDuration)

	tasks, err := uc.repo.ClaimNextDispatched(ctx, tenantID, workerID, limit, leaseExpiresAt, now)
	if err != nil {
		return 0, fmt.Errorf("claim dispatched: %w", err)
	}
	if len(tasks) == 0 {
		return 0, nil
	}

	var wg sync.WaitGroup

	for _, task := range tasks {
		wg.Add(1)
		go func(t AssignedTask) {
			defer wg.Done()
			uc.executeTask(ctx, tenantID, workerID, t, leaseDuration)
		}(task)
	}

	wg.Wait()
	return len(tasks), nil
}

func (uc *TickUseCase) executeTask(
	ctx context.Context,
	tenantID, workerID string,
	task AssignedTask,
	leaseDuration time.Duration,
) {
	handler, ok := uc.handlers[task.HandlerType]
	if !ok {
		slog.Error("unknown handler type",
			"instance_id", task.InstanceID,
			"handler_type", task.HandlerType,
		)
		metrics.ExecutionsTotal.WithLabelValues(task.HandlerType, "unknown_handler").Inc()
		uc.completeAsFailure(ctx, tenantID, task.InstanceID, workerID, task,
			"unknown_handler", fmt.Sprintf("no handler registered for type %q", task.HandlerType))
		return
	}

	metrics.ExecutionsActive.Inc()
	defer metrics.ExecutionsActive.Dec()

	stopRenew := uc.startLeaseRenewal(ctx, tenantID, task.InstanceID, workerID, leaseDuration)

	timeoutDur := time.Duration(task.TimeoutSec) * time.Second
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, timeoutDur)
	var result Result
	func() {
		defer func() {
			if r := recover(); r != nil {
				result = Result{
					Success:    false,
					ResultCode: "panic",
					ErrorMsg:   fmt.Sprintf("handler panic: %v\n%s", r, debug.Stack()),
				}
			}
		}()
		result = handler.Execute(timeoutCtx, task)
	}()
	cancelTimeout()

	stopRenew()

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
		slog.Error("normalize complete failed",
			"instance_id", task.InstanceID,
			"error", err.Error(),
		)
		return
	}

	if err := uc.repo.CompleteInstance(ctx, completeSpec); err != nil {
		slog.Error("complete instance failed",
			"instance_id", task.InstanceID,
			"error", err.Error(),
		)
	}
}

func (uc *TickUseCase) completeAsFailure(
	ctx context.Context,
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
	if err := uc.repo.CompleteInstance(ctx, spec); err != nil {
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
