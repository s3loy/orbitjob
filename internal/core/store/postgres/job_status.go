package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainjob "orbitjob/internal/core/domain/job"
	tenant "orbitjob/internal/core/domain/tenant"
)

// ChangeStatus persists pause/resume lifecycle changes with optimistic concurrency and audit writes.
func (r *JobRepository) ChangeStatus(
	ctx context.Context,
	in domainjob.ChangeStatusSpec,
	changedBy string,
) (_ domainjob.Snapshot, err error) {
	diffBytes, err := json.Marshal(buildStatusDiffPayload(in))
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("marshal status diff_payload: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("begin job status tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Set tenant context for RLS
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", in.TenantID); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	var out domainjob.Snapshot
	var nextRunAt sql.NullTime

	err = tx.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = $4,
		    version = version + 1
		WHERE tenant_id = $1
		  AND id = $2
		  AND version = $3
		  AND status = $5
		  AND deleted_at IS NULL
		RETURNING id, name, tenant_id, status, version, next_run_at, created_at, updated_at
	`,
		in.TenantID,
		in.ID,
		in.Version,
		in.NextStatus,
		in.CurrentStatus,
	).Scan(
		&out.ID,
		&out.Name,
		&out.TenantID,
		&out.Status,
		&out.Version,
		&nextRunAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainjob.Snapshot{}, classifyJobWriteFailure(ctx, tx, in.TenantID, in.ID)
		}

		return domainjob.Snapshot{}, fmt.Errorf("change job status: %w", err)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`,
		in.TenantID,
		tenant.ActorTypeAPIKey,
		changedBy,
		tenant.EventTypeJobStatusChanged,
		tenant.ResourceTypeJob,
		fmt.Sprintf("%d", in.ID),
		string(diffBytes),
	); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("insert audit event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("commit job status tx: %w", err)
	}

	if nextRunAt.Valid {
		t := nextRunAt.Time
		out.NextRunAt = &t
	}

	return out, nil
}

func buildStatusDiffPayload(in domainjob.ChangeStatusSpec) map[string]any {
	return map[string]any{
		"from_version": in.Version,
		"to_version":   in.Version + 1,
		"from_status":  in.CurrentStatus,
		"to_status":    in.NextStatus,
	}
}
