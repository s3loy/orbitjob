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
func (r *BudgetReadRepository) GetCurrent(ctx context.Context, tenantID string, sloID int64) (slo.Budget, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return slo.Budget{}, fmt.Errorf("begin budget get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var budget slo.Budget
	err = tx.QueryRowContext(ctx, `
			SELECT id, tenant_id, slo_id, window_start, window_end, budget_total, budget_consumed, budget_remaining, burn_rate, status, version
			FROM budgets
			WHERE tenant_id = $1 AND slo_id = $2
			ORDER BY window_start DESC
			LIMIT 1
		`, tenantID, sloID).Scan(
		&budget.ID, &budget.TenantID, &budget.SLOID, &budget.WindowStart, &budget.WindowEnd,
		&budget.BudgetTotal, &budget.BudgetConsumed, &budget.BudgetRemaining, &budget.BurnRate,
		&budget.Status, &budget.Version,
	)
	if err == sql.ErrNoRows {
		return budget, &resource.NotFoundError{Resource: "budget", ID: sloID}
	}
	if err != nil {
		return budget, fmt.Errorf("get current budget: %w", err)
	}

	_ = tx.Commit()
	return budget, nil
}

// ListHistory retrieves historical budgets for an SLO.
func (r *BudgetReadRepository) ListHistory(ctx context.Context, tenantID string, sloID int64, limit, offset int) ([]slo.Budget, int64, error) {
	tx, err := WithTenant(ctx, r.db, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("begin budget list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int64
	if err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM budgets WHERE tenant_id = $1 AND slo_id = $2
		`, tenantID, sloID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count budgets: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
			SELECT id, tenant_id, slo_id, window_start, window_end, budget_total, budget_consumed, budget_remaining, burn_rate, status, version
			FROM budgets
			WHERE tenant_id = $1 AND slo_id = $2
			ORDER BY window_start DESC
			LIMIT $3 OFFSET $4
		`, tenantID, sloID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list budgets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var budgets []slo.Budget
	for rows.Next() {
		var b slo.Budget
		err := rows.Scan(
			&b.ID, &b.TenantID, &b.SLOID, &b.WindowStart, &b.WindowEnd,
			&b.BudgetTotal, &b.BudgetConsumed, &b.BudgetRemaining, &b.BurnRate,
			&b.Status, &b.Version,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan budget: %w", err)
		}
		budgets = append(budgets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate budgets: %w", err)
	}

	_ = tx.Commit()
	return budgets, total, nil
}
