package schedule

import (
	"context"
	"time"

	domain "orbitjob/internal/core/domain"
)

type batchScheduler interface {
	ScheduleBatch(
		ctx context.Context,
		now time.Time,
		limit int,
		decide func(time.Time, DueCronJob) (ScheduleDecision, error),
		classifyError func(error) domain.ErrorClass,
	) (BatchCounts, error)
}

// TickUseCase executes one bounded scheduler batch.
type TickUseCase struct {
	repo          batchScheduler
	classifyError func(error) domain.ErrorClass
}

func NewTickUseCase(repo batchScheduler, classifyFn func(error) domain.ErrorClass) *TickUseCase {
	return &TickUseCase{repo: repo, classifyError: classifyFn}
}

// BatchCounts reports per-category counts from one RunBatch invocation.
type BatchCounts struct {
	Handled   int
	Scheduled int
	Skipped   int // SkipWorthy errors
	Backoff   int // BackoffWorthy errors
	Fatal     int // FatalWorthy errors (always 0 or 1 — terminates batch)
}

// RunBatch processes at most limit due cron jobs with three-category error handling.
// FatalWorthy errors terminate the batch immediately.
// SkipWorthy and BackoffWorthy errors are counted but the loop continues.
func (uc *TickUseCase) RunBatch(ctx context.Context, now time.Time, limit int) (BatchCounts, error) {
	if limit < 1 {
		limit = 1
	}
	return uc.repo.ScheduleBatch(ctx, now, limit, DecideSchedule, uc.classifyError)
}
