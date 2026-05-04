package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	domaininstance "orbitjob/internal/core/domain/instance"
	tenant "orbitjob/internal/core/domain/tenant"
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

	// Set tenant context for RLS
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", claimSpec.TenantID); err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("set tenant context: %w", err)
	}

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

	// Step 2: Lock job row and lookup concurrency_policy.
	// FOR UPDATE prevents concurrent dispatchers for the same job from racing
	// through the policy check + count + dispatch in parallel (forbid race).
	var concurrencyPolicy string
	err = tx.QueryRowContext(ctx, `
		SELECT concurrency_policy FROM jobs
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		FOR UPDATE
	`, candidate.TenantID, candidate.JobID).Scan(&concurrencyPolicy)
	if err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("lookup concurrency policy: %w", err)
	}

	// Step 3: Count dispatched+running instances for same job.
	var runningCount int
	err = tx.QueryRowContext(ctx, `
		SELECT count(*) FROM job_instances
		WHERE tenant_id = $1 AND job_id = $2 AND status IN ('dispatched', 'running')
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
		updated, err = updateInstanceToDispatched(ctx, tx, candidate, claimSpec)
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
		updated, err = updateInstanceToDispatched(ctx, tx, candidate, claimSpec)
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
		       status, priority, effective_priority, partition_key,
		       idempotency_key, idempotency_scope, routing_key, worker_id,
		       attempt, max_attempt, scheduled_at, started_at,
		       finished_at, lease_expires_at, dispatched_at, retry_at,
		       result_code, error_msg, trace_id, created_at, updated_at
		FROM job_instances
		WHERE tenant_id = $1
		  AND (status = 'pending' OR (
		       status = 'retry_wait'
		       AND retry_at IS NOT NULL
		       AND retry_at <= $2
		       AND attempt < max_attempt))
		ORDER BY effective_priority DESC, scheduled_at ASC, id ASC
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

func updateInstanceToDispatched(ctx context.Context, tx *sql.Tx, snap domaininstance.Snapshot, spec domaininstance.ClaimSpec) (domaininstance.Snapshot, error) {
	row := tx.QueryRowContext(ctx, `
		UPDATE job_instances
		SET status = 'dispatched',
		    dispatched_at = $1,
		    effective_priority = LEAST(GREATEST(priority,
		        priority + FLOOR(EXTRACT(EPOCH FROM ($1 - scheduled_at)) / 60)::int),
		        priority + 60),
		    lease_expires_at = $2,
		    retry_at = NULL,
		    started_at = NULL,
		    finished_at = NULL,
		    result_code = NULL,
		    error_msg = NULL,
		    attempt = CASE WHEN status = 'retry_wait' THEN attempt + 1 ELSE attempt END
		WHERE id = $3
		RETURNING id, run_id::text, tenant_id, job_id, trigger_source,
		          status, priority, effective_priority, partition_key,
		          idempotency_key, idempotency_scope, routing_key, worker_id,
		          attempt, max_attempt, scheduled_at, started_at,
		          finished_at, lease_expires_at, dispatched_at, retry_at,
		          result_code, error_msg, trace_id, created_at, updated_at
	`, spec.Now, spec.LeaseExpiresAt, snap.ID)

	out, err := scanInstanceSnapshot(row)
	if err != nil {
		return domaininstance.Snapshot{}, err
	}

	diffBytes, err := json.Marshal(map[string]any{
		"from_status": snap.Status,
		"to_status":   "dispatched",
		"job_id":      snap.JobID,
	})
	if err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("marshal audit diff: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`,
		snap.TenantID,
		tenant.ActorTypeSystem,
		"dispatcher",
		tenant.EventTypeInstanceStatusChanged,
		tenant.ResourceTypeInstance,
		snap.RunID,
		string(diffBytes),
	); err != nil {
		return domaininstance.Snapshot{}, fmt.Errorf("insert audit event: %w", err)
	}

	return out, nil
}

func cancelRunningInstances(ctx context.Context, tx *sql.Tx, tenantID string, jobID int64, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `
		UPDATE job_instances
		SET status = 'canceled',
		    finished_at = $3,
		    error_msg = 'canceled by concurrency replace policy'
		WHERE tenant_id = $1 AND job_id = $2
		  AND status IN ('dispatched', 'running')
		RETURNING run_id::text, status
	`, tenantID, jobID, now)
	if err != nil {
		return fmt.Errorf("cancel running instances: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var runID, prevStatus string
		if err := rows.Scan(&runID, &prevStatus); err != nil {
			return fmt.Errorf("scan canceled instance: %w", err)
		}
		diffBytes, err := json.Marshal(map[string]any{
			"from_status": prevStatus,
			"to_status":   "canceled",
			"job_id":      jobID,
		})
		if err != nil {
			return fmt.Errorf("marshal audit diff: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`,
			tenantID,
			tenant.ActorTypeSystem,
			"dispatcher",
			tenant.EventTypeInstanceStatusChanged,
			tenant.ResourceTypeInstance,
			runID,
			string(diffBytes),
		); err != nil {
			return fmt.Errorf("insert audit event: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate canceled instances: %w", err)
	}
	return nil
}

// RecoverLeaseOrphans reclaims dispatched instances whose lease has expired
// (dispatcher crashed after claiming) AND running instances whose lease has
// expired (worker crashed). Returns counts for each recovery category.
func (r *DispatchRepository) RecoverLeaseOrphans(ctx context.Context, now time.Time) (dispatched, running int64, _ error) {
	// Phase 1: dispatched orphans → pending
	dRows, err := r.db.QueryContext(ctx, `
		UPDATE job_instances
		SET status = 'pending',
		    worker_id = NULL,
		    lease_expires_at = NULL,
		    dispatched_at = NULL,
		    error_msg = 'orphaned: dispatcher lease expired'
		WHERE status = 'dispatched'
		  AND lease_expires_at < $1
		RETURNING run_id::text, tenant_id
	`, now)
	if err != nil {
		return 0, 0, fmt.Errorf("recover dispatched orphans: %w", err)
	}
	defer func() { _ = dRows.Close() }()

	for dRows.Next() {
		var runID, tenantID string
		if err := dRows.Scan(&runID, &tenantID); err != nil {
			return 0, 0, fmt.Errorf("scan dispatched orphan: %w", err)
		}
		dispatched++

		diffBytes, err := json.Marshal(map[string]any{
			"from_status": "dispatched",
			"to_status":   "pending",
		})
		if err != nil {
			return 0, 0, fmt.Errorf("marshal audit diff: %w", err)
		}
		if _, err = r.db.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`,
			tenantID,
			tenant.ActorTypeSystem,
			"dispatcher",
			tenant.EventTypeOrphanRecovered,
			tenant.ResourceTypeInstance,
			runID,
			string(diffBytes),
		); err != nil {
			return 0, 0, fmt.Errorf("insert audit event: %w", err)
		}
	}
	if err := dRows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate dispatched orphans: %w", err)
	}

	// Phase 2: running orphans → retry_wait or failed
	rRows, err := r.db.QueryContext(ctx, `
		UPDATE job_instances ji
		SET status = CASE
		        WHEN ji.attempt < ji.max_attempt THEN 'retry_wait'::VARCHAR
		        ELSE 'failed'::VARCHAR
		    END,
		    worker_id = NULL,
		    lease_expires_at = NULL,
		    finished_at = $1,
		    retry_at = CASE
		        WHEN ji.attempt < ji.max_attempt
		        THEN $1 + make_interval(secs => COALESCE(j.retry_backoff_sec, 0))
		        ELSE NULL
		    END,
		    error_msg = CASE
		        WHEN ji.attempt < ji.max_attempt THEN 'orphaned: worker lease expired, retrying'
		        ELSE 'orphaned: worker lease expired, no retries left'
		    END
		FROM jobs j
		WHERE ji.tenant_id = j.tenant_id
		  AND ji.job_id = j.id
		  AND ji.status = 'running'
		  AND ji.lease_expires_at < $1
		RETURNING ji.run_id::text, ji.tenant_id
	`, now)
	if err != nil {
		return dispatched, 0, fmt.Errorf("recover running orphans: %w", err)
	}
	defer func() { _ = rRows.Close() }()

	for rRows.Next() {
		var runID, tenantID string
		if err := rRows.Scan(&runID, &tenantID); err != nil {
			return dispatched, 0, fmt.Errorf("scan running orphan: %w", err)
		}
		running++

		diffBytes, err := json.Marshal(map[string]any{
			"from_status": "running",
			"to_status":   "orphan_recovered",
		})
		if err != nil {
			return dispatched, 0, fmt.Errorf("marshal audit diff: %w", err)
		}
		if _, err = r.db.ExecContext(ctx, `
			INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`,
			tenantID,
			tenant.ActorTypeSystem,
			"dispatcher",
			tenant.EventTypeOrphanRecovered,
			tenant.ResourceTypeInstance,
			runID,
			string(diffBytes),
		); err != nil {
			return dispatched, 0, fmt.Errorf("insert audit event: %w", err)
		}
	}
	if err := rRows.Err(); err != nil {
		return dispatched, 0, fmt.Errorf("iterate running orphans: %w", err)
	}
	return dispatched, running, nil
}

// RefreshEffectivePriority recomputes the materialized effective_priority
// for all pending and retry_wait instances.
func (r *DispatchRepository) RefreshEffectivePriority(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE job_instances
		SET effective_priority = LEAST(
		    GREATEST(priority,
		        priority + FLOOR(EXTRACT(EPOCH FROM ($1 - scheduled_at)) / 60)::int),
		    priority + 60)
		WHERE status IN ('pending', 'retry_wait')
	`, now)
	if err != nil {
		return 0, fmt.Errorf("refresh effective priority: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("refresh rows affected: %w", err)
	}
	if n > 0 {
		diffBytes, err := json.Marshal(map[string]any{"affected_rows": n, "refreshed_at": now})
		if err != nil {
			slog.Error("marshal audit diff failed", "error", err.Error())
		} else if _, err = r.db.ExecContext(ctx, `
			INSERT INTO audit_events (
				tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff
			) VALUES ('system', $1, $2, $3, $4, $5, $6::jsonb)
		`, tenant.ActorTypeSystem, "dispatcher", tenant.EventTypeInstanceStatusChanged,
			tenant.ResourceTypeAudit, "effective_priority_refresh", string(diffBytes)); err != nil {
			slog.Error("insert audit event failed", "error", err.Error())
		}
	}
	return n, nil
}
