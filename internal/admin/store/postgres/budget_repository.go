package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
)

// BudgetReadRepository provides read-side access to budgets table.
type BudgetReadRepository struct {
	db *sql.DB
}

// NewBudgetReadRepository creates a new read repository.
func NewBudgetReadRepository(db *sql.DB) *BudgetReadRepository {
	return &BudgetReadRepository{db: db}
}

// GetCurrent retrieves the current budget for an SLO.
func (r *BudgetReadRepository) GetCurrent(ctx context.Context, tenantID, resourceGroupID string, sloID int64) (slo.Budget, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return slo.Budget{}, fmt.Errorf("begin budget get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var budget slo.Budget
	var evaluated sql.NullTime
	err = tx.QueryRowContext(ctx, `
			SELECT id, tenant_id, slo_id, window_start, window_end, budget_total, budget_consumed, budget_remaining, burn_rate, status, version,evaluated_at,last_event_at,last_sample_at,statistics_semantics,evaluation_reason
			FROM budgets
			WHERE tenant_id = $1 AND slo_id = $2
              AND ($3::text IS NULL OR EXISTS (
                SELECT 1 FROM slos s WHERE s.tenant_id = $1 AND s.id = budgets.slo_id AND s.resource_group_id = $3))
			ORDER BY window_start DESC
			LIMIT 1
		`, tenantID, sloID, nullableGroup(resourceGroupID)).Scan(
		&budget.ID, &budget.TenantID, &budget.SLOID, &budget.WindowStart, &budget.WindowEnd,
		&budget.BudgetTotal, &budget.BudgetConsumed, &budget.BudgetRemaining, &budget.BurnRate,
		&budget.Status, &budget.Version, &evaluated, &budget.LastEventAt, &budget.LastSampleAt, &budget.StatisticsSemantics, &budget.EvaluationReason,
	)
	if err == sql.ErrNoRows {
		if err := requireVisibleSLO(ctx, tx, tenantID, resourceGroupID, sloID); err != nil {
			return budget, err
		}
		return budget, &resource.NotFoundError{Resource: "budget", ID: sloID}
	}
	if err != nil {
		return budget, fmt.Errorf("get current budget: %w", err)
	}

	_ = tx.Commit()
	if evaluated.Valid {
		budget.EvaluatedAt = evaluated.Time
	}
	return budget, nil
}

// ListHistory retrieves historical budgets for an SLO.
func (r *BudgetReadRepository) ListHistory(ctx context.Context, tenantID, resourceGroupID string, sloID int64, limit, offset int) ([]slo.Budget, int64, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("begin budget list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int64
	if err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM budgets WHERE tenant_id = $1 AND slo_id = $2
              AND ($3::text IS NULL OR EXISTS (
                SELECT 1 FROM slos s WHERE s.tenant_id = $1 AND s.id = budgets.slo_id AND s.resource_group_id = $3))
		`, tenantID, sloID, nullableGroup(resourceGroupID)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count budgets: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
			SELECT id, tenant_id, slo_id, window_start, window_end, budget_total, budget_consumed, budget_remaining, burn_rate, status, version,evaluated_at,last_event_at,last_sample_at,statistics_semantics,evaluation_reason
			FROM budgets
			WHERE tenant_id = $1 AND slo_id = $2
              AND ($3::text IS NULL OR EXISTS (
                SELECT 1 FROM slos s WHERE s.tenant_id = $1 AND s.id = budgets.slo_id AND s.resource_group_id = $3))
			ORDER BY window_start DESC
			LIMIT $4 OFFSET $5
		`, tenantID, sloID, nullableGroup(resourceGroupID), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list budgets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var budgets []slo.Budget
	for rows.Next() {
		var b slo.Budget
		var evaluated sql.NullTime
		err := rows.Scan(
			&b.ID, &b.TenantID, &b.SLOID, &b.WindowStart, &b.WindowEnd,
			&b.BudgetTotal, &b.BudgetConsumed, &b.BudgetRemaining, &b.BurnRate,
			&b.Status, &b.Version, &evaluated, &b.LastEventAt, &b.LastSampleAt, &b.StatisticsSemantics, &b.EvaluationReason,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan budget: %w", err)
		}
		if evaluated.Valid {
			b.EvaluatedAt = evaluated.Time
		}
		budgets = append(budgets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate budgets: %w", err)
	}

	_ = tx.Commit()
	return budgets, total, nil
}

// Distinguish a visible SLO with no evaluation yet from an invisible SLO.
func requireVisibleSLO(ctx context.Context, tx *sql.Tx, tenantID, groupID string, id int64) error {
	var visible bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM slos WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL AND ($3::text IS NULL OR resource_group_id = $3))`, tenantID, id, nullableGroup(groupID)).Scan(&visible); err != nil {
		return fmt.Errorf("check budget SLO visibility: %w", err)
	}
	if !visible {
		return &resource.NotFoundError{Resource: "slo", ID: id}
	}
	return nil
}
