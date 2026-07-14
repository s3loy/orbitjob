package postgres

import (
	"context"
	"database/sql"
	"fmt"

	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	"orbitjob/internal/domain/resource"
)

// budgetAlertRow is the internal scan target for budget_alerts queries.
type budgetAlertRow struct {
	ID          int64
	TenantID    string
	SLOID       int64
	BudgetID    int64
	AlertType   string
	BurnRate    float64
	Status      string
	TriggeredAt sql.NullTime
	ResolvedAt  sql.NullTime
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
func (r *BudgetAlertReadRepository) Get(ctx context.Context, tenantID string, id int64) (sloalertquery.BudgetAlertItem, error) {
	var row budgetAlertRow

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, slo_id, budget_id, alert_type, burn_rate, status, triggered_at, resolved_at
		FROM budget_alerts
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(
		&row.ID, &row.TenantID, &row.SLOID, &row.BudgetID, &row.AlertType,
		&row.BurnRate, &row.Status, &row.TriggeredAt, &row.ResolvedAt,
	)
	if err == sql.ErrNoRows {
		return sloalertquery.BudgetAlertItem{}, &resource.NotFoundError{Resource: "budget_alert", ID: id}
	}
	if err != nil {
		return sloalertquery.BudgetAlertItem{}, fmt.Errorf("get budget alert: %w", err)
	}
	return mapBudgetAlertRow(row), nil
}

// List retrieves a paginated list of budget alerts.
func (r *BudgetAlertReadRepository) List(ctx context.Context, tenantID string, sloID *int64, status *string, limit, offset int) ([]sloalertquery.BudgetAlertItem, int64, error) {
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

	var alerts []sloalertquery.BudgetAlertItem
	for rows.Next() {
		var row budgetAlertRow
		err := rows.Scan(
			&row.ID, &row.TenantID, &row.SLOID, &row.BudgetID, &row.AlertType,
			&row.BurnRate, &row.Status, &row.TriggeredAt, &row.ResolvedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan budget alert: %w", err)
		}
		alerts = append(alerts, mapBudgetAlertRow(row))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate budget alerts: %w", err)
	}

	return alerts, total, nil
}

func mapBudgetAlertRow(row budgetAlertRow) sloalertquery.BudgetAlertItem {
	item := sloalertquery.BudgetAlertItem{
		ID:        row.ID,
		TenantID:  row.TenantID,
		SLOID:     row.SLOID,
		BudgetID:  row.BudgetID,
		AlertType: row.AlertType,
		BurnRate:  row.BurnRate,
		Status:    row.Status,
	}
	if row.TriggeredAt.Valid {
		item.TriggeredAt = row.TriggeredAt.Time
	}
	if row.ResolvedAt.Valid {
		t := row.ResolvedAt.Time
		item.ResolvedAt = &t
	}
	return item
}
