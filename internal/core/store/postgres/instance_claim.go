package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domaininstance "orbitjob/internal/core/domain/instance"
)

func (r *InstanceRepository) ClaimNextRunnable(
	ctx context.Context,
	in domaininstance.ClaimSpec,
) (domaininstance.Snapshot, bool, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id
			FROM job_instances
			WHERE tenant_id = $1
			  AND (
			    status = 'pending'
			    OR (
			    	status = 'retry_wait'
			    	AND retry_at IS NOT NULL
			    	AND retry_at <= $2
			    	AND attempt < max_attempt
			    )
			  )
			ORDER BY priority DESC, scheduled_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE job_instances AS ji
		SET status = 'dispatching',
		    worker_id = $3,
		    lease_expires_at = $4,
		    retry_at = NULL,
		    started_at = NULL,
		    finished_at = NULL,
		    result_code = NULL,
		    error_msg = NULL,
		    attempt = CASE
		    	WHEN ji.status = 'retry_wait' THEN ji.attempt + 1
		    	ELSE ji.attempt
		    END
		FROM candidate
		WHERE ji.id = candidate.id
		RETURNING
			ji.id,
			ji.run_id::text,
			ji.tenant_id,
			ji.job_id,
			ji.trigger_source,
			ji.status,
			ji.priority,
			ji.partition_key,
			ji.idempotency_key,
			ji.idempotency_scope,
			ji.routing_key,
			ji.worker_id,
			ji.attempt,
			ji.max_attempt,
			ji.scheduled_at,
			ji.started_at,
			ji.finished_at,
			ji.lease_expires_at,
			ji.retry_at,
			ji.result_code,
			ji.error_msg,
			ji.trace_id,
			ji.created_at,
			ji.updated_at
	`,
		in.TenantID,
		in.Now,
		in.WorkerID,
		in.LeaseExpiresAt,
	)

	out, err := scanInstanceSnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domaininstance.Snapshot{}, false, nil
	}
	if err != nil {
		return domaininstance.Snapshot{}, false, fmt.Errorf("claim job instance: %w", err)
	}

	return out, true, nil
}
