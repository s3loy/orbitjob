package checkschedule

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/platform/metrics"
)

// TickUseCase schedules checks by scanning for due checks and creating check runs.
type TickUseCase struct {
	checkRepo    checkRepository
	checkRunRepo checkRunRepository
	clock        func() time.Time
}

// NewTickUseCase creates a new check scheduling use case.
func NewTickUseCase(checkRepo checkRepository, checkRunRepo checkRunRepository) *TickUseCase {
	return &TickUseCase{
		checkRepo:    checkRepo,
		checkRunRepo: checkRunRepo,
		clock:        func() time.Time { return time.Now().UTC() },
	}
}

type checkRepository interface {
	ListDue(ctx context.Context, tenantID string, now time.Time, limit int) ([]check.Snapshot, error)
	UpdateNextRunAt(ctx context.Context, tenantID string, id int64, nextRunAt time.Time) error
}

type checkRunRepository interface {
	Create(ctx context.Context, tenantID string, checkID int64, scheduledAt time.Time) (any, error)
}

// RunBatch scans for due checks and creates check runs.
func (uc *TickUseCase) RunBatch(ctx context.Context, tenantID string, limit int) (int, error) {
	now := uc.clock()

	checks, err := uc.checkRepo.ListDue(ctx, tenantID, now, limit)
	if err != nil {
		return 0, fmt.Errorf("list due checks: %w", err)
	}

	var created int
	for _, c := range checks {
		if _, err := uc.checkRunRepo.Create(ctx, tenantID, c.ID, now); err != nil {
			slog.Error("failed to create check run", "check_id", c.ID, "error", err)
			continue
		}
		created++

		// Compute next run time.
		var nextRunAt time.Time
		if c.ScheduleType == "cron" && c.CronExpr != nil {
			// For MVP, just use a simple interval-based next run for cron.
			// In production, this should use the cron parser.
			nextRunAt = now.Add(time.Minute)
		} else if c.ScheduleType == "interval" && c.IntervalSec != nil {
			nextRunAt = now.Add(time.Duration(*c.IntervalSec) * time.Second)
		} else {
			nextRunAt = now.Add(time.Minute)
		}

		if err := uc.checkRepo.UpdateNextRunAt(ctx, tenantID, c.ID, nextRunAt); err != nil {
			slog.Error("failed to update check next_run_at", "check_id", c.ID, "error", err)
		}
	}

	if created > 0 {
		metrics.CheckRunsCreatedTotal.WithLabelValues(tenantID).Add(float64(created))
	}

	return created, nil
}
