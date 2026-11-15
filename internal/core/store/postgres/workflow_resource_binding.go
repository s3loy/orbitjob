package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"orbitjob/internal/domain/resource"
)

// BindResource records the first successfully published or observed resource
// UID. A later instance with the same name cannot take over this execution.
func (r *WorkflowRunRepository) BindResource(ctx context.Context, tenantID string, id int64, uid string) error {
	if uid == "" {
		return fmt.Errorf("workflow resource UID is required")
	}
	return r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var current, actor string
		if err := tx.QueryRowContext(ctx, `SELECT resource_uid, actor FROM workflow_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&current, &actor); err != nil {
			return err
		}
		if current == uid {
			return nil
		}
		if current != "" {
			return &resource.ConflictError{Resource: "workflow run resource", ID: id, Message: "WorkflowRun UID differs from the original recorded instance"}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_run_control_plane SET resource_uid=$1 WHERE tenant_id=$2 AND id=$3 AND resource_uid=''`, uid, tenantID, id); err != nil {
			return err
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, actor, "workflow_run.resource_bound", auditResourceWorkflowRun, strconv.FormatInt(id, 10), map[string]any{"resource_uid": uid})
	})
}
