package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
)

// DispatchRepository owns dispatcher-side persistence operations.
type DispatchRepository struct {
	db *sql.DB
}

func NewDispatchRepository(db *sql.DB) *DispatchRepository {
	return &DispatchRepository{db: db}
}

// DispatchOne claims one pending/retry_wait instance, looks up the job's concurrency
// policy, counts running instances, calls the decide function, and executes the
// resulting action — all within a single database transaction.
func (r *DispatchRepository) DispatchOne(
	ctx context.Context,
	claimSpec domaininstance.ClaimSpec,
	decide func(domaininstance.DispatchInput) domaininstance.DispatchDecision,
) (_ domaininstance.Snapshot, found bool, _ error) {
	if decide == nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("decide policy is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("begin dispatch tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Step 1: Claim one candidate instance.
	candidate, found, err := claimOneDispatchCandidate(ctx, tx, claimSpec)
	if err != nil {
		return domaininstance.Snapshot{}, false, err
	}
	if !found {
		if rbErr := tx.Rollback(); rbErr != nil {
			return domaininstance.Snapshot{}, false, fmt.Errorf("rollback empty dispatch tx: %w", rbErr)
		}
		return domaininstance.Snapshot{}, false, nil
	}

	// Step 2: Lookup job's concurrency_policy.
	var concurrencyPolicy string
	err = tx.QueryRowContext(ctx, `
		SELECT concurrency_policy FROM jobs
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, candidate.TenantID, candidate.JobID).Scan(&concurrencyPolicy)
	if err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("lookup concurrency policy: %w", err)
	}

	// Step 3: Count dispatching+running instances for same job.
	var runningCount int
	err = tx.QueryRowContext(ctx, `
		SELECT count(*) FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2 AND status IN ('dispatching', 'running')
	`, candidate.TenantID, candidate.JobID).Scan(&runningCount)
	if err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("count running instances: %w", err)
	}

	// Step 4: Decide.
	decision := decide(domaininstance.DispatchInput{
		InstanceSnapshot:  candidate,
		ConcurrencyPolicy: concurrencyPolicy,
		RunningCount:      runningCount,
	})

	// Step 5: Execute decision.
	switch decision.Action {
	case domaininstance.DispatchActionDispatch:
		var updated domaininstance.Snapshot
		updated, err = updateInstanceToDispatching(ctx, tx, candidate, claimSpec)
		if err != nil {
			return domaininstance.Snapshot{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return domaininstance.Snapshot{}, false, fmt.Errorf("commit dispatch tx: %w", err)
		}
		return updated, true, nil

	case domaininstance.DispatchActionSkip:
		if rbErr := tx.Rollback(); rbErr != nil {
			return domaininstance.Snapshot{}, false, fmt.Errorf("rollback skip dispatch tx: %w", rbErr)
		}
		// Skip: candidate stays pending, return not-found so the loop continues.
		return domaininstance.Snapshot{}, false, nil

	case domaininstance.DispatchActionReplace:
		// Cancel existing running instances.
		err = cancelRunningInstances(ctx, tx, candidate.TenantID, candidate.JobID, claimSpec.Now)
		if err != nil {
			return domaininstance.Snapshot{}, false, err
		}
		var updated domaininstance.Snapshot
		updated, err = updateInstanceToDispatching(ctx, tx, candidate, claimSpec)
		if err != nil {
			return domaininstance.Snapshot{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return domaininstance.Snapshot{}, false, fmt.Errorf("commit dispatch tx: %w", err)
		}
		return updated, true, nil

	default:
		_ = tx.Rollback()
		return domaininstance.Snapshot{}, false, fmt.Errorf("unknown dispatch action: %s", decision.Action)
	}
}

func claimOneDispatchCandidate(ctx context.Context, tx *sql.Tx, spec domaininstance.ClaimSpec) (domaininstance.Snapshot, bool, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, run_id::text, tenant_id, job_id, trigger_source,
		       status, priority, partition_key, idempotency_key,
		       idempotency_scope, routing_key, worker_id,
		       attempt, max_attempt, scheduled_at, started_at,
		       finished_at, lease_expires_at, retry_at,
		       result_code, error_msg, trace_id, created_at, updated_at
		FROM job_instances
		WHERE tenant_id = $1
		  AND (status = 'pending' OR (
		       status = 'retry_wait'
		       AND retry_at IS NOT NULL
		       AND retry_at <= $2
		       AND attempt < max_attempt))
		ORDER BY priority DESC, scheduled_at ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, spec.TenantID, spec.Now)

	out, err := scanInstanceSnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domaininstance.Snapshot{}, false, nil
	}
	if err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("claim dispatch candidate: %w", err)
	}
	return out, true, nil
}

func updateInstanceToDispatching(ctx context.Context, tx *sql.Tx, snap domaininstance.Snapshot, spec domaininstance.ClaimSpec) (domaininstance.Snapshot, error) {
	row := tx.QueryRowContext(ctx, `
		UPDATE job_instances
		SET status = 'dispatching',
		    worker_id = $1,
		    lease_expires_at = $2,
		    retry_at = NULL,
		    started_at = NULL,
		    finished_at = NULL,
		    result_code = NULL,
		    error_msg = NULL,
		    attempt = CASE WHEN status = 'retry_wait' THEN attempt + 1 ELSE attempt END
		WHERE id = $3
		RETURNING id, run_id::text, tenant_id, job_id, trigger_source,
		          status, priority, partition_key, idempotency_key,
		          idempotency_scope, routing_key, worker_id,
		          attempt, max_attempt, scheduled_at, started_at,
		          finished_at, lease_expires_at, retry_at,
		          result_code, error_msg, trace_id, created_at, updated_at
	`, spec.WorkerID, spec.LeaseExpiresAt, snap.ID)

	return scanInstanceSnapshot(row)
}

func cancelRunningInstances(ctx context.Context, tx *sql.Tx, tenantID string, jobID int64, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE job_instances
		SET status = 'canceled',
		    finished_at = $3,
		    error_msg = 'canceled by concurrency replace policy'
		WHERE tenant_id = $1 AND job_id = $2
		  AND status IN ('dispatching', 'running')
	`, tenantID, jobID, now)
	if err != nil {
		return fmt.Errorf("cancel running instances: %w", err)
	}
	return nil
}
