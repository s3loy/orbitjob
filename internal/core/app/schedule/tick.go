package schedule

import (
	"context"
	"log/slog"
	"time"

	domain "orbitjob/internal/core/domain"
)

type oneJobScheduler interface {
	ScheduleOneDueCron(
		ctx context.Context,
		now time.Time,
		decide func(time.Time, DueCronJob) (ScheduleDecision, error),
	) (ScheduledOneResult, bool, error)
}

// TickUseCase executes one bounded scheduler batch.
type TickUseCase struct {
	repo          oneJobScheduler
	classifyError func(error) domain.ErrorClass
}

func NewTickUseCase(repo oneJobScheduler, classifyFn func(error) domain.ErrorClass) *TickUseCase {
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

	counts := BatchCounts{}
	for i := 0; i < limit; i++ {
		result, found, err := uc.repo.ScheduleOneDueCron(ctx, now, DecideSchedule)
		if err != nil {
			class := uc.classifyError(err)
			switch class {
			case domain.FatalWorthy:
				counts.Fatal++
				counts.Handled++
				return counts, nil
			case domain.BackoffWorthy:
				counts.Backoff++
				counts.Handled++
				continue
			case domain.SkipWorthy:
				counts.Skipped++
				counts.Handled++
				continue
			default:
				return counts, err
			}
		}
		if !found {
			break
		}
		counts.Handled++
		if result.Created {
			counts.Scheduled++
		}
		if result.TraceID != "" {
			slog.InfoContext(ctx, "instance scheduled",
				"trace_id", result.TraceID,
				"run_id", result.RunID,
				"job_id", result.JobID,
			)
		}
	}

	return counts, nil
}
