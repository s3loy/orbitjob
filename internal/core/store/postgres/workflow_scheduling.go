package postgres

import (
	"context"
	"database/sql"
	"errors"

	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

// guardNewWorkflowRun validates planned-time eligibility after the shared
// source lock and replay lookup. Concurrency belongs to checkSourceAdmission.
func guardNewWorkflowRun(ctx context.Context, tx *sql.Tx, tenantID string, run workflow.Run) error {
	if run.ScheduledFor != nil {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM job_definition_revisions
   WHERE id=$1 AND tenant_id=$2 AND source_uid=$3 AND source_mode='workflow'
   AND is_active AND COALESCE(activated_at,created_at) <= $4 FOR SHARE`, run.RevisionID, tenantID, run.SourceUID, run.ScheduledFor).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return &resource.ConflictError{Resource: "workflow", ID: run.SourceUID, Message: "scheduled revision is inactive or was not active at the planned time"}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
