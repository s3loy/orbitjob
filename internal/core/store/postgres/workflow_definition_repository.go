package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

// WorkflowDefinitionRepository reads the workflow-definition projection out of
// job_definition_revisions: the active revision of every source_mode='workflow'
// row in the tenant, with the task DAG decoded from the normalized spec. The
// rows are written by the operator's WorkflowJob projection; this repository is
// the admin API's read model over them, the way the jobs read path serves
// ScheduledJob revisions. Every method runs inside the tenant GUC so the RLS
// policy is the second gate.
type WorkflowDefinitionRepository struct {
	db *sql.DB
}

func NewWorkflowDefinitionRepository(db *sql.DB) *WorkflowDefinitionRepository {
	return &WorkflowDefinitionRepository{db: db}
}

// WorkflowDefinitionRow is one active workflow revision as the projection
// stores it: the revision's identity fields plus the decoded WorkflowJobSpec
// the walker and the admin reads share.
type WorkflowDefinitionRow struct {
	ID         int64
	Name       string
	Namespace  string
	SourceUID  string
	Generation int64
	Spec       v1alpha1.WorkflowJobSpec
	CreatedAt  time.Time
}

const workflowDefinitionColumns = `
id, source_name, source_namespace, source_uid, generation, normalized_spec::text, created_at`

func scanWorkflowDefinition(row interface{ Scan(dest ...any) error }) (WorkflowDefinitionRow, error) {
	var (
		out WorkflowDefinitionRow
		raw []byte
	)
	if err := row.Scan(
		&out.ID, &out.Name, &out.Namespace, &out.SourceUID, &out.Generation,
		&raw, &out.CreatedAt,
	); err != nil {
		return WorkflowDefinitionRow{}, err
	}
	// A workflow revision's normalized spec is the full WorkflowJobSpec the
	// projection stored, so a row that does not parse is a write-side defect
	// rather than a missing definition; return the error instead of an empty
	// workflow that would read as "exists, does nothing".
	if err := json.Unmarshal(raw, &out.Spec); err != nil {
		return WorkflowDefinitionRow{}, fmt.Errorf("decode workflow spec: %w", err)
	}
	return out, nil
}

// ListActiveWorkflowDefinitions lists the active revision of every workflow
// definition in the tenant, newest first. The partial unique index
// ux_job_definition_revisions_active guarantees at most one active revision
// per source_uid, so each workflow is listed exactly once. A non-positive
// limit means the default run page; a negative offset is treated as zero.
func (r *WorkflowDefinitionRepository) ListActiveWorkflowDefinitions(ctx context.Context, tenantID string, limit, offset int) ([]WorkflowDefinitionRow, error) {
	if limit < 1 {
		limit = defaultRunPage
	}
	if offset < 0 {
		offset = 0
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin workflow definition list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return nil, fmt.Errorf("set tenant context: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT `+workflowDefinitionColumns+`
		FROM job_definition_revisions
		WHERE tenant_id = $1 AND source_mode = $2 AND is_active
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, tenantID, workflow.SourceModeWorkflow, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list workflow definitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]WorkflowDefinitionRow, 0, limit)
	for rows.Next() {
		def, scanErr := scanWorkflowDefinition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflow definitions: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit workflow definition list: %w", err)
	}
	return out, nil
}

// ActiveWorkflowDefinition reads one workflow definition's active revision by
// revision id, with its task DAG. A revision that does not exist, is not the
// workflow's active one, is not a workflow-sourced revision, or belongs to
// another tenant is NotFoundError -- all four are indistinguishable on
// purpose, because saying which would leak the tenant's or the definition's
// history through the route's own prefix.
func (r *WorkflowDefinitionRepository) ActiveWorkflowDefinition(ctx context.Context, tenantID string, id int64) (WorkflowDefinitionRow, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowDefinitionRow{}, fmt.Errorf("begin workflow definition get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return WorkflowDefinitionRow{}, fmt.Errorf("set tenant context: %w", err)
	}

	def, scanErr := scanWorkflowDefinition(tx.QueryRowContext(ctx, `
		SELECT `+workflowDefinitionColumns+`
		FROM job_definition_revisions
		WHERE tenant_id = $1 AND id = $2 AND source_mode = $3 AND is_active
	`, tenantID, id, workflow.SourceModeWorkflow))
	if errors.Is(scanErr, sql.ErrNoRows) {
		return WorkflowDefinitionRow{}, &resource.NotFoundError{Resource: "workflow", ID: id}
	}
	if scanErr != nil {
		return WorkflowDefinitionRow{}, fmt.Errorf("get workflow definition: %w", scanErr)
	}

	if err = tx.Commit(); err != nil {
		return WorkflowDefinitionRow{}, fmt.Errorf("commit workflow definition get: %w", err)
	}
	return def, nil
}
