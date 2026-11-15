package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/jobrun"
)

// ExecutionIsolationSummary reads unresolved execution from the ledger, rather
// than counting CRs, which may be missing during recovery. The existing
// (tenant_id, phase) index bounds the candidate set to isolated runs.
func (r *ControlPlaneRepository) ExecutionIsolationSummary(ctx context.Context, tenant string) ([]jobrun.IsolationSummary, error) {
	var out []jobrun.IsolationSummary
	err := r.WithTenantTransaction(ctx, tenant, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT r.phase, count(*), min(COALESCE(a.execution_unknown_since, r.updated_at))
			FROM job_run_control_plane r
			LEFT JOIN job_run_attempts_control_plane a
			  ON a.tenant_id = r.tenant_id AND a.run_id = r.id AND a.attempt_number = r.attempt
			WHERE r.tenant_id = $1 AND r.phase IN ('ExecutionUnknown', 'CancelUnknown')
			GROUP BY r.phase`, tenant)
		if err != nil {
			return fmt.Errorf("query execution isolation: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var item jobrun.IsolationSummary
			if err := rows.Scan(&item.Phase, &item.Count, &item.OldestAt); err != nil {
				return fmt.Errorf("scan execution isolation: %w", err)
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
