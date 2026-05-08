package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	domainjob "orbitjob/internal/core/domain/job"
	tenant "orbitjob/internal/core/domain/tenant"
)

func (r *JobRepository) Delete(ctx context.Context, tenantID string, id int64) (domainjob.Snapshot, error) {
	diffBytes, err := json.Marshal(map[string]any{"action": "delete"})
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("marshal delete diff: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("begin job delete tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	var out domainjob.Snapshot

	err = tx.QueryRowContext(ctx, `
		UPDATE jobs
		SET deleted_at = now()
		WHERE tenant_id = $1
		  AND id = $2
		  AND deleted_at IS NULL
		RETURNING id, name, tenant_id, status, version, created_at, updated_at
	`, tenantID, id).Scan(
		&out.ID,
		&out.Name,
		&out.TenantID,
		&out.Status,
		&out.Version,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return domainjob.Snapshot{}, classifyJobWriteFailure(ctx, tx, tenantID, id)
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO audit_events (tenant_id, actor_type, actor_id, event_type, resource_type, resource_id, diff)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
	`,
		tenantID,
		tenant.ActorTypeAPIKey,
		"api_key",
		"job.deleted",
		tenant.ResourceTypeJob,
		fmt.Sprintf("%d", id),
		string(diffBytes),
	); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("insert audit event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return domainjob.Snapshot{}, fmt.Errorf("commit job delete tx: %w", err)
	}

	return out, nil
}
