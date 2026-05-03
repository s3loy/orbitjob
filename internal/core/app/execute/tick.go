package execute

import (
	"context"
	"fmt"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
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

func (uc *TickUseCase) RunOnce(ctx context.Context, tenantID, workerID string, leaseDuration time.Duration) (int, error) {
	now := time.Now()
	leaseExpiresAt := now.Add(leaseDuration)

	tasks, err := uc.repo.ClaimNextDispatched(ctx, tenantID, workerID, 1, leaseExpiresAt, now)
	if err != nil {
		return 0, fmt.Errorf("claim dispatched: %w", err)
	}
	if len(tasks) == 0 {
		return 0, nil
	}
	task := tasks[0]

	handler, ok := uc.handlers[task.HandlerType]
	if !ok {
		uc.completeAsFailure(ctx, tenantID, task.InstanceID, workerID, task,
			"unknown_handler", fmt.Sprintf("no handler registered for type %q", task.HandlerType))
		return 1, nil
	}

	stopRenew := uc.startLeaseRenewal(ctx, tenantID, task.InstanceID, workerID, leaseDuration)

	timeoutDur := time.Duration(task.TimeoutSec) * time.Second
	timeoutCtx, cancelTimeout := context.WithTimeout(ctx, timeoutDur)
	result := handler.Execute(timeoutCtx, task)
	cancelTimeout()

	stopRenew()

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
		return 1, fmt.Errorf("normalize complete: %w", err)
	}

	if err := uc.repo.CompleteInstance(ctx, completeSpec); err != nil {
		return 1, fmt.Errorf("complete instance: %w", err)
	}

	return 1, nil
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
	_ = uc.repo.CompleteInstance(ctx, spec)
}

func (uc *TickUseCase) startLeaseRenewal(
	ctx context.Context,
	tenantID string, instanceID int64, workerID string,
	leaseDuration time.Duration,
) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})

	interval := leaseDuration / 3
	if interval < time.Second {
		interval = time.Second
	}

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
				_ = uc.repo.ExtendLease(ctx, tenantID, instanceID, workerID, newExpiry)
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}
