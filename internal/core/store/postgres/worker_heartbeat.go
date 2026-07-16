package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	domainworker "orbitjob/internal/core/domain/worker"
)

func (r *WorkerRepository) UpsertHeartbeat(
	ctx context.Context,
	in domainworker.HeartbeatSpec,
) (domainworker.Snapshot, error) {
	labelsBytes, err := json.Marshal(in.Labels)
	if err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("marshal worker labels: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("begin worker upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Set tenant context for RLS
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", in.TenantID); err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO workers (
			worker_id,
			tenant_id,
			status,
			last_heartbeat_at,
			lease_expires_at,
			capacity,
			labels
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (tenant_id, worker_id)
		DO UPDATE
		SET status = EXCLUDED.status,
		    last_heartbeat_at = EXCLUDED.last_heartbeat_at,
		    lease_expires_at = EXCLUDED.lease_expires_at,
		    capacity = EXCLUDED.capacity,
		    labels = EXCLUDED.labels
		RETURNING
			tenant_id,
			worker_id,
			status,
			last_heartbeat_at,
			lease_expires_at,
			capacity,
			labels,
			created_at,
			updated_at
	`,
		in.WorkerID,
		in.TenantID,
		in.Status,
		in.LastHeartbeatAt,
		in.LeaseExpiresAt,
		in.Capacity,
		string(labelsBytes),
	)

	out, err := scanWorkerSnapshot(row)
	if err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("upsert worker heartbeat: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("commit worker upsert tx: %w", err)
	}
	return out, nil
}

func (r *WorkerRepository) GetByID(
	ctx context.Context,
	tenantID, workerID string,
) (domainworker.Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("begin worker get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Set tenant context for RLS
	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("set tenant context: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		SELECT
			tenant_id,
			worker_id,
			status,
			last_heartbeat_at,
			lease_expires_at,
			capacity,
			labels,
			created_at,
			updated_at
		FROM workers
		WHERE tenant_id = $1 AND worker_id = $2
	`, tenantID, workerID)

	out, err := scanWorkerSnapshot(row)
	if err != nil {
		return domainworker.Snapshot{}, fmt.Errorf("get worker by id: %w", err)
	}
	_ = tx.Commit()
	return out, nil
}
