package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"orbitjob/internal/core/app/schedule"
	domain "orbitjob/internal/core/domain"
	tenant "orbitjob/internal/core/domain/tenant"
	"orbitjob/internal/platform/metrics"
	"orbitjob/internal/platform/scan"
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

// scheduleOneJobInTx processes a single due job within an existing transaction.
// It returns the result, a flag indicating whether the job was processed (not quota-skipped),
// and any error.
func scheduleOneJobInTx(
	ctx context.Context,
	tx *sql.Tx,
	now time.Time,
	job dueCronJobRecord,
	decide func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error),
) (schedule.ScheduledOneResult, bool, error) {
	// Set tenant context for RLS
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", job.TenantID); err != nil {
		return schedule.ScheduledOneResult{}, false, fmt.Errorf("set tenant context: %w", err)
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

	var runID, traceID string
	if decision.CreateInstance {
		if decision.ScheduledAt == nil {
			return schedule.ScheduledOneResult{}, false, fmt.Errorf("scheduled_at is required when CreateInstance=true")
		}

		if exceeded, err := checkConcurrentInstanceQuota(ctx, tx, job.TenantID); err != nil {
			return schedule.ScheduledOneResult{}, false, err
		} else if exceeded {
			// Quota exceeded: cursor still advances, but no instance created.
			err = updateJobScheduleCursor(ctx, tx, job, decision.NextRunAt, decision.ScheduledAt)
			if err != nil {
				return schedule.ScheduledOneResult{}, false, err
			}
			return schedule.ScheduledOneResult{JobID: job.ID, TenantID: job.TenantID, Created: false, NextRunAt: decision.NextRunAt}, false, nil
		}

		runID, traceID, err = insertScheduledInstance(ctx, tx, job, *decision.ScheduledAt)
		if err != nil {
			return schedule.ScheduledOneResult{}, false, err
		}
		metrics.ScheduleLag.Observe(now.Sub(*decision.ScheduledAt).Seconds())

		diffBytes, err := json.Marshal(map[string]any{
			"job_id":         job.ID,
			"trigger_source": "schedule",
			"scheduled_at":   decision.ScheduledAt,
		})
		if err != nil {
			return schedule.ScheduledOneResult{}, false, fmt.Errorf("marshal audit diff: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`,
			job.TenantID,
			tenant.ActorTypeSystem,
			"scheduler",
			tenant.EventTypeInstanceCreated,
			tenant.ResourceTypeInstance,
			runID,
			string(diffBytes),
		); err != nil {
			return schedule.ScheduledOneResult{}, false, fmt.Errorf("insert audit event: %w", err)
		}
	}

	if err := updateJobScheduleCursor(ctx, tx, job, decision.NextRunAt, decision.ScheduledAt); err != nil {
		return schedule.ScheduledOneResult{}, false, err
	}

	return schedule.ScheduledOneResult{
		JobID:     job.ID,
		TenantID:  job.TenantID,
		RunID:     runID,
		TraceID:   traceID,
		Created:   decision.CreateInstance,
		NextRunAt: decision.NextRunAt,
	}, true, nil
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

	result, processed, err := scheduleOneJobInTx(ctx, tx, now, job, decide)
	if err != nil {
		return schedule.ScheduledOneResult{}, false, err
	}
	if !processed {
		// Quota exceeded — rollback since no instance was created and cursor was updated.
		if rbErr := tx.Rollback(); rbErr != nil {
			return schedule.ScheduledOneResult{}, false, fmt.Errorf("rollback on quota exceeded: %w", rbErr)
		}
		return result, false, nil
	}

	if err = tx.Commit(); err != nil {
		return schedule.ScheduledOneResult{}, false, fmt.Errorf("commit scheduler tx: %w", err)
	}

	return result, true, nil
}

// ScheduleBatch claims and processes up to limit due cron jobs in sub-batches.
// Each sub-batch uses a single transaction with SAVEPOINT per job for isolation.
func (r *SchedulerRepository) ScheduleBatch(
	ctx context.Context,
	now time.Time,
	limit int,
	decide func(time.Time, schedule.DueCronJob) (schedule.ScheduleDecision, error),
	classifyError func(error) domain.ErrorClass,
) (schedule.BatchCounts, error) {
	if decide == nil {
		return schedule.BatchCounts{}, fmt.Errorf("decide policy is required")
	}
	if limit < 1 {
		limit = 1
	}

	const subBatchSize = 50
	counts := schedule.BatchCounts{}

	for remaining := limit; remaining > 0; {
		batchLimit := min(remaining, subBatchSize)

		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			class := classifyError(err)
			if class == domain.FatalWorthy {
				counts.Fatal++
				counts.Handled++
				return counts, nil
			}
			counts.Backoff++
			counts.Handled++
			return counts, nil
		}

		jobs, err := claimMultipleDueCronJobs(ctx, tx, now, batchLimit)
		if err != nil {
			_ = tx.Rollback()
			class := classifyError(err)
			if class == domain.FatalWorthy {
				counts.Fatal++
				counts.Handled++
				return counts, nil
			}
			counts.Backoff++
			counts.Handled++
			return counts, nil
		}
		if len(jobs) == 0 {
			_ = tx.Rollback()
			return counts, nil
		}

		for i, job := range jobs {
			spName := fmt.Sprintf("sp_job_%d", i)
			if _, err := tx.ExecContext(ctx, "SAVEPOINT "+spName); err != nil {
				_ = tx.Rollback()
				counts.Backoff++
				counts.Handled++
				return counts, nil
			}

			result, processed, jobErr := scheduleOneJobInTx(ctx, tx, now, job, decide)
			if jobErr != nil {
				if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+spName); rbErr != nil {
					_ = tx.Rollback()
					counts.Backoff++
					counts.Handled++
					return counts, nil
				}
				class := classifyError(jobErr)
				switch class {
				case domain.FatalWorthy:
					counts.Fatal++
					counts.Handled++
					_ = tx.Rollback()
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
					_ = tx.Rollback()
					return counts, jobErr
				}
			}

			if !processed {
				// Quota exceeded — cursor was updated, keep it.
				if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+spName); err != nil {
					_ = tx.Rollback()
					counts.Backoff++
					counts.Handled++
					return counts, nil
				}
				counts.Handled++
				continue
			}

			if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+spName); err != nil {
				_ = tx.Rollback()
				counts.Backoff++
				counts.Handled++
				return counts, nil
			}

			counts.Handled++
			if result.Created {
				counts.Scheduled++
				if result.TraceID != "" {
					slog.InfoContext(ctx, "instance scheduled",
						"trace_id", result.TraceID,
						"run_id", result.RunID,
						"job_id", result.JobID,
					)
				}
			}
		}

		if err := tx.Commit(); err != nil {
			class := classifyError(err)
			if class == domain.FatalWorthy {
				counts.Fatal++
				counts.Handled++
				return counts, nil
			}
			counts.Backoff++
			counts.Handled++
			return counts, nil
		}

		remaining -= len(jobs)
		if len(jobs) < batchLimit {
			break
		}
	}

	return counts, nil
}

func claimMultipleDueCronJobs(ctx context.Context, tx *sql.Tx, now time.Time, limit int) ([]dueCronJobRecord, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, tenant_id, priority, partition_key, retry_limit, cron_expr, timezone, misfire_policy, next_run_at
		FROM jobs
		WHERE status = 'active'
		  AND trigger_type = 'cron'
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= $1
		  AND deleted_at IS NULL
		ORDER BY next_run_at ASC, priority DESC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim multiple due cron jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []dueCronJobRecord
	for rows.Next() {
		var out dueCronJobRecord
		var partitionKey sql.NullString
		if err := rows.Scan(
			&out.ID,
			&out.TenantID,
			&out.Priority,
			&partitionKey,
			&out.RetryLimit,
			&out.CronExpr,
			&out.Timezone,
			&out.MisfirePolicy,
			&out.NextRunAt,
		); err != nil {
			return nil, fmt.Errorf("scan due cron job: %w", err)
		}
		out.PartitionKey = scan.NullStringPtr(partitionKey)
		jobs = append(jobs, out)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due cron jobs: %w", err)
	}
	return jobs, nil
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

	out.PartitionKey = scan.NullStringPtr(partitionKey)
	return out, true, nil
}

func insertScheduledInstance(ctx context.Context, tx *sql.Tx, job dueCronJobRecord, scheduledAt time.Time) (string, string, error) {
	maxAttempt := job.RetryLimit + 1
	traceID := uuid.New().String()

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
			max_attempt,
				trace_id
		)
		VALUES ($1, $2, 'schedule', $3, 'pending', $4, $4, $5, 'job_instance_create', 1, $6, $7)
		RETURNING run_id::text
	`,
		job.TenantID,
		job.ID,
		scheduledAt,
		job.Priority,
		job.PartitionKey,
		maxAttempt,
		traceID,
	).Scan(&runID)
	if err != nil {
		return "", "", fmt.Errorf("insert scheduled instance: %w", err)
	}

	return runID, traceID, nil
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

func checkConcurrentInstanceQuota(ctx context.Context, tx *sql.Tx, tenantID string) (bool, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT quotas FROM tenants WHERE id = $1`, tenantID).Scan(&raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read tenant quotas: %w", err)
	}
	if len(raw) == 0 {
		return false, nil
	}
	var quotas map[string]any
	if err := json.Unmarshal(raw, &quotas); err != nil {
		return false, fmt.Errorf("unmarshal tenant quotas: %w", err)
	}
	v, ok := quotas["max_concurrent_instances"]
	if !ok {
		return false, nil
	}
	maxConc, ok := toFloatInt(v)
	if !ok || maxConc <= 0 {
		return false, nil
	}
	var count int
	err = tx.QueryRowContext(ctx, `
		SELECT count(*) FROM job_instances
		WHERE tenant_id = $1 AND status IN ('pending', 'retry_wait', 'dispatched', 'running')
	`, tenantID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("count concurrent instances: %w", err)
	}
	return count >= maxConc, nil
}

// CountActiveInstances returns the total number of instances in active states
// (pending, retry_wait, dispatched, running) across all tenants.
func (r *SchedulerRepository) CountActiveInstances(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM job_instances
		WHERE status IN ('pending', 'retry_wait', 'dispatched', 'running')
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active instances: %w", err)
	}
	return count, nil
}

// ListActiveTenantIDs returns IDs of tenants with status = 'active'.
func (r *SchedulerRepository) ListActiveTenantIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM orbitjob_list_active_tenant_ids()`)
	if err != nil {
		return nil, fmt.Errorf("list active tenant ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tenant id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant rows: %w", err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

func toFloatInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	default:
		return 0, false
	}
}
