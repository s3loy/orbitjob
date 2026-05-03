package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"orbitjob/internal/core/app/schedule"
)

// SchedulerRepository owns scheduler-side persistence operations.
type SchedulerRepository struct {
	db *sql.DB
}

type dueCronJobRecord struct {
	ID            int64
	TenantID      string
	Priority      int
	PartitionKey  *string
	RetryLimit    int
	CronExpr      string
	Timezone      string
	MisfirePolicy string
	NextRunAt     time.Time
}

func NewSchedulerRepository(db *sql.DB) *SchedulerRepository {
	return &SchedulerRepository{db: db}
}

// ScheduleOneDueCron claims one due cron job, applies scheduling policy, and persists cursor/instance atomically.
func (r *SchedulerRepository) ScheduleOneDueCron(
	ctx context.Context,
	now time.Time,
	decide func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error),
) (_ schedule.ScheduledOneResult, found bool, err error) {
	if decide == nil {
		return schedule.ScheduledOneResult{}, false, fmt.Errorf("decide policy is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return schedule.ScheduledOneResult{}, false, fmt.Errorf("begin scheduler tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	job, found, err := claimOneDueCronJob(ctx, tx, now)
	if err != nil {
		return schedule.ScheduledOneResult{}, false, err
	}
	if !found {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return schedule.ScheduledOneResult{}, false, fmt.Errorf("rollback empty scheduler tx: %w", rollbackErr)
		}
		return schedule.ScheduledOneResult{}, false, nil
	}

	decision, err := decide(now, schedule.DueCronJob{
		CronExpr:      job.CronExpr,
		Timezone:      job.Timezone,
		MisfirePolicy: job.MisfirePolicy,
		NextRunAt:     job.NextRunAt,
	})
	if err != nil {
		return schedule.ScheduledOneResult{}, false, fmt.Errorf("decide schedule policy: %w", err)
	}

	var runID string
	if decision.CreateInstance {
		if decision.ScheduledAt == nil {
			return schedule.ScheduledOneResult{}, false, fmt.Errorf("scheduled_at is required when CreateInstance=true")
		}

		runID, err = insertScheduledInstance(ctx, tx, job, *decision.ScheduledAt)
		if err != nil {
			return schedule.ScheduledOneResult{}, false, err
		}
	}

	err = updateJobScheduleCursor(ctx, tx, job, decision.NextRunAt, decision.ScheduledAt)
	if err != nil {
		return schedule.ScheduledOneResult{}, false, err
	}

	if err = tx.Commit(); err != nil {
		return schedule.ScheduledOneResult{}, false, fmt.Errorf("commit scheduler tx: %w", err)
	}

	return schedule.ScheduledOneResult{
		JobID:     job.ID,
		TenantID:  job.TenantID,
		RunID:     runID,
		Created:   decision.CreateInstance,
		NextRunAt: decision.NextRunAt,
	}, true, nil
}

func claimOneDueCronJob(ctx context.Context, tx *sql.Tx, now time.Time) (dueCronJobRecord, bool, error) {
	var (
		out          dueCronJobRecord
		partitionKey sql.NullString
	)

	err := tx.QueryRowContext(ctx, `
		SELECT id, tenant_id, priority, partition_key, retry_limit, cron_expr, timezone, misfire_policy, next_run_at
		FROM jobs
		WHERE status = 'active'
		  AND trigger_type = 'cron'
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= $1
		  AND deleted_at IS NULL
		ORDER BY next_run_at ASC, priority DESC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, now).Scan(
		&out.ID,
		&out.TenantID,
		&out.Priority,
		&partitionKey,
		&out.RetryLimit,
		&out.CronExpr,
		&out.Timezone,
		&out.MisfirePolicy,
		&out.NextRunAt,
	)
	if err == sql.ErrNoRows {
		return dueCronJobRecord{}, false, nil
	}
	if err != nil {
		return dueCronJobRecord{}, false, fmt.Errorf("claim one due cron job: %w", err)
	}

	out.PartitionKey = nullStringPtr(partitionKey)
	return out, true, nil
}

func insertScheduledInstance(ctx context.Context, tx *sql.Tx, job dueCronJobRecord, scheduledAt time.Time) (string, error) {
	maxAttempt := job.RetryLimit + 1

	var runID string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO job_instances (
			tenant_id,
			job_id,
			trigger_source,
			scheduled_at,
			status,
			priority,
			effective_priority,
			partition_key,
			idempotency_scope,
			attempt,
			max_attempt
		)
		VALUES ($1, $2, 'schedule', $3, 'pending', $4, $4, $5, 'job_instance_create', 1, $6)
		RETURNING run_id::text
	`,
		job.TenantID,
		job.ID,
		scheduledAt,
		job.Priority,
		job.PartitionKey,
		maxAttempt,
	).Scan(&runID)
	if err != nil {
		return "", fmt.Errorf("insert scheduled instance: %w", err)
	}

	return runID, nil
}

func updateJobScheduleCursor(
	ctx context.Context,
	tx *sql.Tx,
	job dueCronJobRecord,
	nextRunAt *time.Time,
	lastScheduledAt *time.Time,
) error {
	if nextRunAt == nil {
		return fmt.Errorf("next_run_at is required")
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET next_run_at = $3,
		    last_scheduled_at = COALESCE($4, last_scheduled_at)
		WHERE tenant_id = $1
		  AND id = $2
	`,
		job.TenantID,
		job.ID,
		nextRunAt,
		lastScheduledAt,
	)
	if err != nil {
		return fmt.Errorf("update job schedule cursor: %w", err)
	}

	return nil
}
