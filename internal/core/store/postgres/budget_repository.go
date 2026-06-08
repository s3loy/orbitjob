package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
)

// BudgetRepository manages error budget tracking.
type BudgetRepository struct {
	db *sql.DB
}

// NewBudgetRepository creates a new budget repository.
func NewBudgetRepository(db *sql.DB) *BudgetRepository {
	return &BudgetRepository{db: db}
}

// Upsert creates or updates a budget record for an SLO window.
// Returns the budget with its persisted ID.
func (r *BudgetRepository) Upsert(ctx context.Context, tenantID string, budget slo.Budget) (slo.Budget, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return budget, fmt.Errorf("begin upsert tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return budget, fmt.Errorf("set tenant context: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO budgets (tenant_id, slo_id, window_start, window_end, budget_total, budget_consumed, budget_remaining, burn_rate, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, slo_id, window_start)
		DO UPDATE SET
			budget_total = EXCLUDED.budget_total,
			budget_consumed = EXCLUDED.budget_consumed,
			budget_remaining = EXCLUDED.budget_remaining,
			burn_rate = EXCLUDED.burn_rate,
			status = EXCLUDED.status,
			version = budgets.version + 1,
			updated_at = now()
		RETURNING id
	`, tenantID, budget.SLOID, budget.WindowStart, budget.WindowEnd,
		budget.BudgetTotal, budget.BudgetConsumed, budget.BudgetRemaining,
		budget.BurnRate, budget.Status).Scan(&id)
	if err != nil {
		return budget, fmt.Errorf("upsert budget: %w", err)
	}
	budget.ID = id

	if err = tx.Commit(); err != nil {
		return budget, fmt.Errorf("commit upsert budget: %w", err)
	}

	return budget, nil
}

// GetCurrent returns the current budget for an SLO (most recent window).
func (r *BudgetRepository) GetCurrent(ctx context.Context, tenantID string, sloID int64) (slo.Budget, error) {
	var budget slo.Budget

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return budget, fmt.Errorf("begin get tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return budget, fmt.Errorf("set tenant context: %w", err)
	}

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

	if err = tx.Commit(); err != nil {
		return budget, fmt.Errorf("commit get budget: %w", err)
	}

	return budget, nil
}
