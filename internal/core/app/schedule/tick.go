package schedule

import (
	"context"
	"time"
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
	repo oneJobScheduler
}

func NewTickUseCase(repo oneJobScheduler) *TickUseCase {
	return &TickUseCase{repo: repo}
}

// RunBatch handles at most limit due jobs in one tick.
func (uc *TickUseCase) RunBatch(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit < 1 {
		limit = 1
	}

	handled := 0
	for i := 0; i < limit; i++ {
		_, found, err := uc.repo.ScheduleOneDueCron(ctx, now, DecideSchedule)
		if err != nil {
			return handled, err
		}
		if !found {
			break
		}
		handled++
	}

	return handled, nil
}
