package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// BudgetAlertItem represents a budget alert for the read model.
type BudgetAlertItem struct {
	ID          int64     `json:"id"`
	TenantID    string    `json:"tenant_id"`
	SLOID       int64     `json:"slo_id"`
	BudgetID    int64     `json:"budget_id"`
	AlertType   string    `json:"alert_type"`
	BurnRate    float64   `json:"burn_rate"`
	Status      string    `json:"status"`
	TriggeredAt time.Time `json:"triggered_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

// BudgetAlertReadRepository provides read-side access to budget_alerts table.
type BudgetAlertReadRepository struct {
	db *sql.DB
}

// NewBudgetAlertReadRepository creates a new read repository.
func NewBudgetAlertReadRepository(db *sql.DB) *BudgetAlertReadRepository {
	return &BudgetAlertReadRepository{db: db}
}

// Get retrieves a budget alert by ID.
func (r *BudgetAlertReadRepository) Get(ctx context.Context, tenantID string, id int64) (BudgetAlertItem, error) {
	var item BudgetAlertItem
	var resolvedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, slo_id, budget_id, alert_type, burn_rate, status, triggered_at, resolved_at
		FROM budget_alerts
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(
		&item.ID, &item.TenantID, &item.SLOID, &item.BudgetID, &item.AlertType,
		&item.BurnRate, &item.Status, &item.TriggeredAt, &resolvedAt,
	)
	if err == sql.ErrNoRows {
		return item, fmt.Errorf("budget alert not found: %d", id)
	}
	if err != nil {
		return item, fmt.Errorf("get budget alert: %w", err)
	}
	if resolvedAt.Valid {
		item.ResolvedAt = &resolvedAt.Time
	}
	return item, nil
}

// List retrieves a paginated list of budget alerts.
func (r *BudgetAlertReadRepository) List(ctx context.Context, tenantID string, sloID *int64, status *string, limit, offset int) ([]BudgetAlertItem, int64, error) {
	var total int64
	countQuery := `SELECT COUNT(*) FROM budget_alerts WHERE tenant_id = $1`
	countArgs := []any{tenantID}
	countArgIdx := 2

	if sloID != nil {
		countQuery += fmt.Sprintf(` AND slo_id = $%d`, countArgIdx)
		countArgs = append(countArgs, *sloID)
		countArgIdx++
	}
	if status != nil {
		countQuery += fmt.Sprintf(` AND status = $%d`, countArgIdx)
		countArgs = append(countArgs, *status)
	}

	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count budget alerts: %w", err)
	}

	listQuery := `
		SELECT id, tenant_id, slo_id, budget_id, alert_type, burn_rate, status, triggered_at, resolved_at
		FROM budget_alerts
		WHERE tenant_id = $1`
	listArgs := []any{tenantID}
	argIdx := 2

	if sloID != nil {
		listQuery += fmt.Sprintf(` AND slo_id = $%d`, argIdx)
		listArgs = append(listArgs, *sloID)
		argIdx++
	}
	if status != nil {
		listQuery += fmt.Sprintf(` AND status = $%d`, argIdx)
		listArgs = append(listArgs, *status)
		argIdx++
	}
	listQuery += fmt.Sprintf(` ORDER BY triggered_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	listArgs = append(listArgs, limit, offset)

	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list budget alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var alerts []BudgetAlertItem
	for rows.Next() {
		var item BudgetAlertItem
		var resolvedAt sql.NullTime
		err := rows.Scan(
			&item.ID, &item.TenantID, &item.SLOID, &item.BudgetID, &item.AlertType,
			&item.BurnRate, &item.Status, &item.TriggeredAt, &resolvedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan budget alert: %w", err)
		}
		if resolvedAt.Valid {
			item.ResolvedAt = &resolvedAt.Time
		}
		alerts = append(alerts, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate budget alerts: %w", err)
	}

	return alerts, total, nil
}
