package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"orbitjob/internal/core/app/sloevaluate"
)

// BudgetAlertRepository manages budget burn rate alerts.
type BudgetAlertRepository struct {
	db *sql.DB
}

// NewBudgetAlertRepository creates a new alert repository.
func NewBudgetAlertRepository(db *sql.DB) *BudgetAlertRepository {
	return &BudgetAlertRepository{db: db}
}

// Create inserts a new budget alert.
func (r *BudgetAlertRepository) Create(ctx context.Context, tenantID string, sloID, budgetID int64, alertType string, burnRate float64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO budget_alerts (tenant_id, slo_id, budget_id, alert_type, burn_rate)
		VALUES ($1, $2, $3, $4, $5)
	`, tenantID, sloID, budgetID, alertType, burnRate)
	if err != nil {
		return fmt.Errorf("create budget alert: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit create alert: %w", err)
	}

	return nil
}

// ResolveBySLOAndType resolves active alerts of a specific type for an SLO.
func (r *BudgetAlertRepository) ResolveBySLOAndType(ctx context.Context, tenantID string, sloID int64, alertType string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin resolve tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE budget_alerts
		SET status = 'resolved', resolved_at = now()
		WHERE tenant_id = $1 AND slo_id = $2 AND alert_type = $3 AND status = 'active'
	`, tenantID, sloID, alertType)
	if err != nil {
		return fmt.Errorf("resolve budget alerts: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit resolve alert: %w", err)
	}

	return nil
}

// GetActiveBySLO returns all active alerts for an SLO.
func (r *BudgetAlertRepository) GetActiveBySLO(ctx context.Context, tenantID string, sloID int64) ([]sloevaluate.AlertSnapshot, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin get tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, alert_type, status
		FROM budget_alerts
		WHERE tenant_id = $1 AND slo_id = $2 AND status = 'active'
		ORDER BY triggered_at DESC
	`, tenantID, sloID)
	if err != nil {
		return nil, fmt.Errorf("query active alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var alerts []sloevaluate.AlertSnapshot
	for rows.Next() {
		var a sloevaluate.AlertSnapshot
		if err := rows.Scan(&a.ID, &a.AlertType, &a.Status); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alerts: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit get alerts: %w", err)
	}

	return alerts, nil
}
