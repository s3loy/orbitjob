package postgres

import (
	"context"
	"database/sql"
	"fmt"

	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

// RunRepository reads the run ledger: job_run_control_plane and its attempt
// trail in job_run_attempts_control_plane.
//
// It is read-only by construction. The operator owns the ledger write, and
// orbitjob_admin holds SELECT and nothing else on the three control-plane
// tables, so a compromised API server cannot forge a run or an attempt. The
// missing INSERT grant is the design, not an omission to repair.
type RunRepository struct {
	db *sql.DB
}

// NewRunRepository creates a RunRepository.
func NewRunRepository(db *sql.DB) *RunRepository {
	return &RunRepository{db: db}
}

// runColumns is the run-level projection, aliased so it is unambiguous inside
// the join used by Get. occurrence_key is CHAR(64); rtrim removes the padding a
// shorter key would carry, so the caller sees the key that was written.
const runColumns = `
	r.id, r.tenant_id, r.source_uid, r.revision_id, rtrim(r.occurrence_key),
	r.trigger, r.actor, r.phase, r.attempt, r.max_attempts, r.created_at, r.updated_at`

// List returns one page of runs for a tenant, newest first.
//
// It is a single round trip. The run list is the product's main query, so the
// attempt trail is deliberately not fetched here: doing it per row would make
// the main query N+1. Callers that need the trail read one run with Get.
func (r *RunRepository) List(ctx context.Context, in runquery.ListInput) ([]runquery.ListItem, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("begin run list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT `+runColumns+`
		FROM job_run_control_plane r
		WHERE r.tenant_id = $1
		  AND ($2 = '' OR r.phase = $2)
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $3 OFFSET $4
	`, in.TenantID, in.Phase, in.Limit, in.Offset)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]runquery.ListItem, 0)
	for rows.Next() {
		var item runquery.ListItem
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.SourceUID,
			&item.RevisionID,
			&item.OccurrenceKey,
			&item.Trigger,
			&item.Actor,
			&item.Phase,
			&item.Attempt,
			&item.MaxAttempts,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run row: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit run list tx: %w", err)
	}
	return out, nil
}

// getRunSQL joins a run to its attempts in one round trip. A run with no attempt
// yet still returns one row, with the attempt columns NULL, so "has not started"
// and "does not exist" stay distinguishable without a second query.
const getRunSQL = `
	SELECT ` + runColumns + `,
	       a.id, a.run_id, a.attempt_number, a.phase, a.kubernetes_job_name,
	       a.kubernetes_job_uid, a.observed_resource_version,
	       a.started_at, a.completed_at, a.created_at
	FROM job_run_control_plane r
	LEFT JOIN job_run_attempts_control_plane a
	  ON a.tenant_id = r.tenant_id AND a.run_id = r.id
	WHERE r.tenant_id = $1 AND r.id = $2
	ORDER BY a.attempt_number`

// Get returns one run with its attempt trail, or NotFoundError when the run is
// not in the caller's tenant.
func (r *RunRepository) Get(ctx context.Context, in runquery.GetInput) (runquery.GetItem, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return runquery.GetItem{}, fmt.Errorf("begin run get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, getRunSQL, in.TenantID, in.ID)
	if err != nil {
		return runquery.GetItem{}, fmt.Errorf("get run: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := runquery.GetItem{Attempts: []runquery.AttemptItem{}}
	found := false
	for rows.Next() {
		var (
			attemptID    sql.NullInt64
			attemptRunID sql.NullInt64
			attemptNo    sql.NullInt64
			attemptPhase sql.NullString
			jobName      sql.NullString
			jobUID       sql.NullString
			observedRV   sql.NullString
			startedAt    sql.NullTime
			completedAt  sql.NullTime
			attemptAt    sql.NullTime
		)
		if err := rows.Scan(
			&out.ID,
			&out.TenantID,
			&out.SourceUID,
			&out.RevisionID,
			&out.OccurrenceKey,
			&out.Trigger,
			&out.Actor,
			&out.Phase,
			&out.Attempt,
			&out.MaxAttempts,
			&out.CreatedAt,
			&out.UpdatedAt,
			&attemptID,
			&attemptRunID,
			&attemptNo,
			&attemptPhase,
			&jobName,
			&jobUID,
			&observedRV,
			&startedAt,
			&completedAt,
			&attemptAt,
		); err != nil {
			return runquery.GetItem{}, fmt.Errorf("scan run detail row: %w", err)
		}
		found = true

		if !attemptID.Valid {
			continue
		}
		out.Attempts = append(out.Attempts, runquery.AttemptItem{
			ID:                      attemptID.Int64,
			RunID:                   attemptRunID.Int64,
			AttemptNumber:           int(attemptNo.Int64),
			Phase:                   attemptPhase.String,
			KubernetesJobName:       jobName.String,
			KubernetesJobUID:        scan.NullStringPtr(jobUID),
			ObservedResourceVersion: scan.NullStringPtr(observedRV),
			StartedAt:               scan.NullTimePtr(startedAt),
			CompletedAt:             scan.NullTimePtr(completedAt),
			CreatedAt:               attemptAt.Time,
		})
	}
	if err := rows.Err(); err != nil {
		return runquery.GetItem{}, fmt.Errorf("iterate run detail rows: %w", err)
	}
	if !found {
		return runquery.GetItem{}, &resource.NotFoundError{Resource: "run", ID: in.ID}
	}
	if err := tx.Commit(); err != nil {
		return runquery.GetItem{}, fmt.Errorf("commit run get tx: %w", err)
	}
	return out, nil
}

// ListAttempts returns the attempt trail for one run in attempt order. The join
// through the run is how the run id is scoped to the tenant: an attempt carries
// its own tenant_id, but the run is what the caller asked for.
func (r *RunRepository) ListAttempts(ctx context.Context, in runquery.GetInput) ([]runquery.AttemptItem, error) {
	tx, err := WithTenant(ctx, r.db, in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("begin attempt list tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, a.run_id, a.attempt_number, a.phase, a.kubernetes_job_name,
		       a.kubernetes_job_uid, a.observed_resource_version,
		       a.started_at, a.completed_at, a.created_at
		FROM job_run_attempts_control_plane a
		JOIN job_run_control_plane r
		  ON r.tenant_id = a.tenant_id AND r.id = a.run_id
		WHERE r.tenant_id = $1 AND r.id = $2
		ORDER BY a.attempt_number
	`, in.TenantID, in.ID)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []runquery.AttemptItem{}
	for rows.Next() {
		var (
			item        runquery.AttemptItem
			jobUID      sql.NullString
			observedRV  sql.NullString
			startedAt   sql.NullTime
			completedAt sql.NullTime
		)
		if err := rows.Scan(
			&item.ID,
			&item.RunID,
			&item.AttemptNumber,
			&item.Phase,
			&item.KubernetesJobName,
			&jobUID,
			&observedRV,
			&startedAt,
			&completedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan attempt row: %w", err)
		}
		item.KubernetesJobUID = scan.NullStringPtr(jobUID)
		item.ObservedResourceVersion = scan.NullStringPtr(observedRV)
		item.StartedAt = scan.NullTimePtr(startedAt)
		item.CompletedAt = scan.NullTimePtr(completedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempt rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit attempt list tx: %w", err)
	}
	return out, nil
}
