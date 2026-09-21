package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// tenantSetting is the GUC the RLS policies bind to. Every policy reads
// current_setting('app.tenant_id', true); using any other name would leave the
// policies matching NULL and silently deny (or, worse, be bypassed by a role
// without RLS) instead of scoping rows to the caller's tenant.
const tenantSetting = "app.tenant_id"

// WithTenant sets the transaction-local tenant GUC. The third argument to
// set_config is true, so the value dies with the transaction and cannot leak to
// the next borrower of this pooled connection.
func WithTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	if tx == nil {
		return fmt.Errorf("nil transaction")
	}
	if tenantID == "" {
		return fmt.Errorf("tenant id is required")
	}
	_, err := tx.ExecContext(ctx, `SELECT set_config($1, $2, true)`, tenantSetting, tenantID)
	return err
}

// WithTenantTransaction runs fn inside a transaction that has the tenant GUC set.
// Any error from fn rolls back; the tenant value never survives the transaction.
func (r *ControlPlaneRepository) WithTenantTransaction(ctx context.Context, tenantID string, fn func(context.Context, *sql.Tx) error) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil database")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}
