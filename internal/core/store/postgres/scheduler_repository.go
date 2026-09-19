package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// SchedulerRepository holds the scheduler-side reads that outlive the cron path.
//
// The claim/decide/persist cycle is gone with the jobs and job_instances
// tables: a Kubernetes declaration is projected into a revision and fired by the
// control plane, so there is no due-cron row to claim. What the scheduler still
// does is run the check scheduler and the SLO evaluator, and both iterate active
// tenants. That enumeration is all this repository exists for.
type SchedulerRepository struct {
	db *sql.DB
}

func NewSchedulerRepository(db *sql.DB) *SchedulerRepository {
	return &SchedulerRepository{db: db}
}

// ListActiveTenantIDs returns the ids of tenants with status = 'active', ordered
// by id so a caller that iterates them does so deterministically.
//
// It calls a SECURITY DEFINER function rather than selecting from tenants
// directly: the caller has to enumerate tenants before it has a tenant to scope
// the row-level security GUC to, and an unscoped read of tenants returns
// nothing.
func (r *SchedulerRepository) ListActiveTenantIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM orbitjob_list_active_tenant_ids()`)
	if err != nil {
		return nil, fmt.Errorf("list active tenant ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := []string{}
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
	return ids, nil
}
