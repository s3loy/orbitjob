package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainjob "orbitjob/internal/core/domain/job"
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
		INSERT INTO job_change_audits (
			tenant_id,
			job_id,
			action,
			changed_by,
			diff_payload
		)
		VALUES ($1, $2, $3, $4, $5::jsonb)
	`,
		in.TenantID,
		in.ID,
		in.Action,
		changedBy,
		string(diffBytes),
	); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("insert job audit: %w", err)
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
