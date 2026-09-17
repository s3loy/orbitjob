package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lib/pq"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	"orbitjob/internal/domain/resource"
)

// Audit vocabulary for the workflow ledger, matching the run-ledger rows in
// control_plane_repository.go. event_type is a VARCHAR(32) column with no
// CHECK, so the set stays closed here.
const (
	auditEventWorkflowRunCreated      = "workflow_run.created"
	auditEventWorkflowRunPhaseChanged = "workflow_run.phase_changed"
	auditEventWorkflowRunPruned       = "workflow_run.pruned"

	auditResourceWorkflowRun = "workflow_run"
)

// WorkflowRunRepository implements WorkflowRunStore against the workflow run
// ledger (workflow_run_control_plane) and the step-grouping column on
// job_run_control_plane. The workflow row is the workflow's state; a step is
// an ordinary job run whose workflow_run_id groups it under that row.
//
// Every operation runs inside a transaction that carries the tenant GUC, so
// the RLS policies in 0005 bind, and every ledger change records itself in
// audit_events in the same transaction. The rollback in withTenantTx is
// unconditional: Rollback after a successful Commit is a no-op, and an error
// return that bypasses an err check still releases the pooled connection
// instead of pinning it with a half-set tenant GUC.
type WorkflowRunRepository struct {
	db  *sql.DB
	now func() time.Time
}

func NewWorkflowRunRepository(db *sql.DB) *WorkflowRunRepository {
	return &WorkflowRunRepository{db: db, now: time.Now}
}

// The repository satisfies the store contract exactly as written; the pin
// turns a signature drift into a build failure instead of a walker bug.
var _ WorkflowRunStore = (*WorkflowRunRepository)(nil)

// withTenantTx runs fn inside a transaction that has the tenant GUC set
// transaction-locally, so the setting cannot outlive the transaction and leak
// to the next borrower of this pooled connection.
func (r *WorkflowRunRepository) withTenantTx(ctx context.Context, tenantID string, fn func(context.Context, *sql.Tx) error) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil database")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// uniqueViolation reports whether err is a PostgreSQL unique violation. The
// dedup inserts below fire it only from constraints the ON CONFLICT clause
// does not name -- a primary key after a sequence-desyncing restore, or a
// constraint a future migration adds -- and a driver error that raw would
// reach the walker undeciphered.
func uniqueViolation(err error) (*pq.Error, bool) {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return pqErr, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Workflow runs
// ---------------------------------------------------------------------------

// insertWorkflowRunSQL creates one workflow run. The dedup identity is the
// database's UNIQUE (source_uid, occurrence_key), not the caller's: two
// writers racing on one occurrence -- a replayed tick, two manual triggers
// carrying the same idempotency key -- must converge on one row. task_decisions
// starts empty; decisions arrive through UpdatePhase's merge.
const insertWorkflowRunSQL = `
INSERT INTO workflow_run_control_plane
  (tenant_id, source_uid, revision_id, occurrence_key, trigger, actor, phase)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (source_uid, occurrence_key) DO NOTHING
RETURNING id`

const workflowRunProjection = `
id, source_uid, revision_id, occurrence_key, trigger, actor, phase,
task_decisions, created_at, updated_at`

const selectWorkflowRunByOccurrenceSQL = `SELECT ` + workflowRunProjection + `
FROM workflow_run_control_plane WHERE source_uid=$1 AND occurrence_key=$2 AND tenant_id=$3`

const selectWorkflowRunByIDSQL = `SELECT ` + workflowRunProjection + `
FROM workflow_run_control_plane WHERE id=$1 AND tenant_id=$2`

const openWorkflowRunsSQL = `SELECT ` + workflowRunProjection + `
FROM workflow_run_control_plane WHERE tenant_id=$1 AND phase <> ALL($2)
ORDER BY id`

// CreateRunForTenant records one workflow run unless the occurrence already
// exists, and always returns the stored row. created is false for the loser
// of a dedup race, whose caller gets the winner's row unchanged: firing the
// same occurrence twice converges on one row, never two, and only the winner
// writes an audit row for it.
func (r *WorkflowRunRepository) CreateRunForTenant(
	ctx context.Context, tenantID string, run workflow.Run,
) (workflow.Run, bool, error) {
	if run.SourceUID == "" || run.OccurrenceKey == "" {
		return workflow.Run{}, false, fmt.Errorf("workflow run identity is required")
	}
	if run.Trigger == "" {
		return workflow.Run{}, false, fmt.Errorf("workflow run trigger is required")
	}
	if run.Phase != "" && run.Phase != workflow.PhasePending {
		return workflow.Run{}, false, fmt.Errorf("a workflow run is created in %s, not %s", workflow.PhasePending, run.Phase)
	}
	if err := validateActor(run.Actor); err != nil {
		return workflow.Run{}, false, err
	}

	var (
		stored  workflow.Run
		created bool
	)
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var id int64
		insertErr := tx.QueryRowContext(ctx, insertWorkflowRunSQL,
			tenantID, run.SourceUID, run.RevisionID, run.OccurrenceKey,
			string(run.Trigger), run.Actor, string(workflow.PhasePending),
		).Scan(&id)
		switch {
		case errors.Is(insertErr, sql.ErrNoRows):
			// The occurrence is already recorded; this writer lost the race.
		case insertErr != nil:
			if pqErr, violated := uniqueViolation(insertErr); violated {
				return &resource.ConflictError{
					Resource: auditResourceWorkflowRun,
					ID:       run.OccurrenceKey,
					Message: fmt.Sprintf("workflow run %s/%s violates unique constraint %s",
						run.SourceUID, run.OccurrenceKey, pqErr.Constraint),
				}
			}
			return insertErr
		default:
			created = true
		}

		out, scanErr := scanWorkflowRunRow(tx.QueryRowContext(ctx,
			selectWorkflowRunByOccurrenceSQL, run.SourceUID, run.OccurrenceKey, tenantID))
		if scanErr != nil {
			return fmt.Errorf("read workflow run occurrence: %w", scanErr)
		}
		stored = out
		if !created {
			return nil
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, run.Actor, auditEventWorkflowRunCreated,
			auditResourceWorkflowRun, strconv.FormatInt(stored.ID, 10), map[string]any{
				"source_uid":     run.SourceUID,
				"revision_id":    run.RevisionID,
				"occurrence_key": run.OccurrenceKey,
				"trigger":        string(run.Trigger),
			})
	})
	if err != nil {
		return workflow.Run{}, false, err
	}
	return stored, created, nil
}

// scanWorkflowRunRow reads one full workflow run row. task_decisions comes
// back as the jsonb text the server normalized, so the decode below sees one
// canonical shape regardless of how the writer merged it.
func scanWorkflowRunRow(row interface{ Scan(dest ...any) error }) (workflow.Run, error) {
	var (
		out          workflow.Run
		trigger      string
		phase        string
		decisionsRaw []byte
	)
	if err := row.Scan(
		&out.ID, &out.SourceUID, &out.RevisionID, &out.OccurrenceKey,
		&trigger, &out.Actor, &phase, &decisionsRaw, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return workflow.Run{}, err
	}
	out.Trigger = jobrun.Trigger(trigger)
	out.Phase = workflow.Phase(phase)
	decisions, err := decodeTaskDecisions(decisionsRaw)
	if err != nil {
		return workflow.Run{}, err
	}
	out.TaskDecisions = decisions
	return out, nil
}

// decodeTaskDecisions decodes the task_decisions column. The column is NOT NULL
// with an object CHECK, so an empty trail arrives as '{}' and decodes to an
// empty, non-nil map rather than a nil one the walker would have to guard.
func decodeTaskDecisions(data []byte) (map[string]workflow.TaskDecision, error) {
	var out map[string]workflow.TaskDecision
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("decode task decisions: %w", err)
		}
	}
	if out == nil {
		out = map[string]workflow.TaskDecision{}
	}
	return out, nil
}

// encodeTaskDecisions renders the decision trail for a jsonb bind parameter.
// A nil or empty map must travel as '{}' and not the JSON null a nil map
// marshals to: 'null'::jsonb does not concatenate.
func encodeTaskDecisions(decisions map[string]workflow.TaskDecision) (string, error) {
	if len(decisions) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(decisions)
	if err != nil {
		return "", fmt.Errorf("encode task decisions: %w", err)
	}
	return string(encoded), nil
}

// decisionsWouldChange reports whether the incoming decisions add anything the
// stored trail does not already hold, per task name. It mirrors the statement's
// own merge predicate in Go so the calm no-op path can be told apart from a
// refusal without a second round trip.
func decisionsWouldChange(stored, incoming map[string]workflow.TaskDecision) bool {
	for name, decision := range incoming {
		if stored[name] != decision {
			return true
		}
	}
	return false
}

// RunByOccurrence loads one workflow run by its dedup identity. found is false
// when the row does not exist or belongs to another tenant -- RLS and the
// tenant predicate make both cases indistinguishable, on purpose.
func (r *WorkflowRunRepository) RunByOccurrence(
	ctx context.Context, tenantID, sourceUID, occurrenceKey string,
) (workflow.Run, bool, error) {
	var (
		out   workflow.Run
		found = true
	)
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		run, scanErr := scanWorkflowRunRow(tx.QueryRowContext(ctx,
			selectWorkflowRunByOccurrenceSQL, sourceUID, occurrenceKey, tenantID))
		if errors.Is(scanErr, sql.ErrNoRows) {
			found = false
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		out = run
		return nil
	})
	if err != nil {
		return workflow.Run{}, false, err
	}
	return out, found, nil
}

// RunByID loads one workflow run by ledger id.
func (r *WorkflowRunRepository) RunByID(ctx context.Context, tenantID string, id int64) (workflow.Run, bool, error) {
	var (
		out   workflow.Run
		found = true
	)
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		run, scanErr := scanWorkflowRunRow(tx.QueryRowContext(ctx,
			selectWorkflowRunByIDSQL, id, tenantID))
		if errors.Is(scanErr, sql.ErrNoRows) {
			found = false
			return nil
		}
		if scanErr != nil {
			return scanErr
		}
		out = run
		return nil
	})
	if err != nil {
		return workflow.Run{}, false, err
	}
	return out, found, nil
}

// OpenRuns lists the tenant's non-terminal workflow runs -- the advance pass's
// input, ordered by id so a walker's decisions are reproducible across ticks.
// CancelUnknown is deliberately open: workflow.Terminal excludes it, so a run
// carrying an unconfirmed stop keeps being walked until it resolves.
func (r *WorkflowRunRepository) OpenRuns(ctx context.Context, tenantID string) ([]workflow.Run, error) {
	var out []workflow.Run
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, openWorkflowRunsSQL, tenantID, terminalWorkflowPhases())
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			run, scanErr := scanWorkflowRunRow(rows)
			if scanErr != nil {
				return scanErr
			}
			out = append(out, run)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// terminalWorkflowPhases is the SQL form of workflow.Terminal, which stays the
// authority on what a finished workflow is. The values match the job-run
// terminal set textually, but each domain names its own.
func terminalWorkflowPhases() []string {
	return []string{
		string(workflow.PhaseSucceeded), string(workflow.PhaseFailed), string(workflow.PhaseCanceled),
	}
}

// workflowRetentionPhases is the set the workflow prune may delete: the
// terminal phases plus CancelUnknown, the same vocabulary the job-run ledger's
// retentionPhases carries. Whether the retainer's window makes pruning an
// unconfirmed stop acceptable is the retainer's policy, not this statement's.
func workflowRetentionPhases() []string {
	return append(terminalWorkflowPhases(), string(workflow.PhaseCancelUnknown))
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

const workflowStepProjection = `
id, workflow_run_id, source_uid, revision_id, occurrence_key, trigger,
phase, attempt, created_at, updated_at`

const selectWorkflowStepSQL = `SELECT ` + workflowStepProjection + `
FROM job_run_control_plane WHERE source_uid=$1 AND occurrence_key=$2 AND tenant_id=$3`

const listWorkflowStepsSQL = `SELECT ` + workflowStepProjection + `
FROM job_run_control_plane WHERE workflow_run_id=$1 AND tenant_id=$2
ORDER BY id`

const selectWorkflowRunIDSQL = `SELECT id FROM workflow_run_control_plane WHERE id=$1 AND tenant_id=$2`

// Steps lists the step runs grouped under one workflow run, ordered by id so a
// walker's decisions are reproducible across ticks.
func (r *WorkflowRunRepository) Steps(ctx context.Context, tenantID string, workflowRunID int64) ([]workflow.StepRun, error) {
	var out []workflow.StepRun
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, listWorkflowStepsSQL, workflowRunID, tenantID)
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var step workflow.StepRun
			if scanErr := rows.Scan(
				&step.ID, &step.WorkflowRunID, &step.SourceUID, &step.RevisionID,
				&step.OccurrenceKey, &step.Trigger, &step.Phase, &step.Attempt,
				&step.CreatedAt, &step.UpdatedAt,
			); scanErr != nil {
				return scanErr
			}
			out = append(out, step)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateStepForTenant inserts one step run and groups it under the workflow
// run in the same transaction: the same row shape, dedup and attempt
// bookkeeping the job run occurrence insert performs, plus the
// workflow_run_id pointer that makes the run a step.
//
// Two gates run before the insert, in the same transaction so neither can be
// outraced by a concurrent prune or projection:
//
//   - the workflow run must exist under this tenant. The FK would accept a
//     foreign tenant's row on its own (referential-integrity checks bypass
//     RLS), so the tenant-scoped read is the gate that keeps a step out of
//     another tenant's workflow.
//   - the step travels as trigger Workflow: a step is an ordinary run of the
//     referenced definition and the trigger is the only other thing that
//     marks it as a step, so the writer, not the caller, closes that
//     vocabulary.
//
// created is false when the step's occurrence key already exists for the
// referenced definition, in which case the stored row is returned unchanged
// -- a replayed tick converges on the winner's row and writes no audit row.
func (r *WorkflowRunRepository) CreateStepForTenant(
	ctx context.Context, tenantID string, workflowRunID int64, step workflow.StepRun, maxAttempts int, actor string,
) (workflow.StepRun, bool, error) {
	if workflowRunID <= 0 {
		return workflow.StepRun{}, false, fmt.Errorf("workflow run id is required")
	}
	if step.SourceUID == "" || step.OccurrenceKey == "" {
		return workflow.StepRun{}, false, fmt.Errorf("step identity is required")
	}
	if step.Trigger != jobrun.Workflow {
		return workflow.StepRun{}, false, fmt.Errorf("a workflow step is created with trigger %s, not %s", jobrun.Workflow, step.Trigger)
	}
	if step.Phase != "" && step.Phase != jobrun.Pending {
		return workflow.StepRun{}, false, fmt.Errorf("a step is created in %s, not %s", jobrun.Pending, step.Phase)
	}
	if err := validateActor(actor); err != nil {
		return workflow.StepRun{}, false, err
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var (
		stored  workflow.StepRun
		created bool
	)
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var knownID int64
		if err := tx.QueryRowContext(ctx, selectWorkflowRunIDSQL, workflowRunID, tenantID).Scan(&knownID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &resource.NotFoundError{Resource: auditResourceWorkflowRun, ID: workflowRunID}
			}
			return fmt.Errorf("read workflow run %d: %w", workflowRunID, err)
		}

		// A step has no scheduled instant of its own; the pointer is the only
		// column the plain occurrence insert does not already carry.
		var id int64
		insertErr := tx.QueryRowContext(ctx, insertOccurrenceSQL,
			tenantID, step.SourceUID, step.RevisionID, step.OccurrenceKey,
			string(step.Trigger), actor, string(jobrun.Pending), maxAttempts,
			nil, workflowRunID,
		).Scan(&id)
		switch {
		case errors.Is(insertErr, sql.ErrNoRows):
			// The step's occurrence is already recorded; this writer lost the race.
		case insertErr != nil:
			if pqErr, violated := uniqueViolation(insertErr); violated {
				return &resource.ConflictError{
					Resource: auditResourceRun,
					ID:       step.OccurrenceKey,
					Message: fmt.Sprintf("workflow step %s/%s violates unique constraint %s",
						step.SourceUID, step.OccurrenceKey, pqErr.Constraint),
				}
			}
			return insertErr
		default:
			created = true
		}

		var out workflow.StepRun
		if scanErr := tx.QueryRowContext(ctx, selectWorkflowStepSQL,
			step.SourceUID, step.OccurrenceKey, tenantID,
		).Scan(
			&out.ID, &out.WorkflowRunID, &out.SourceUID, &out.RevisionID,
			&out.OccurrenceKey, &out.Trigger, &out.Phase, &out.Attempt,
			&out.CreatedAt, &out.UpdatedAt,
		); scanErr != nil {
			return fmt.Errorf("read step occurrence: %w", scanErr)
		}
		stored = out
		if !created {
			return nil
		}
		return insertControlPlaneAudit(ctx, tx, tenantID, actor, auditEventRunCreated,
			auditResourceRun, strconv.FormatInt(stored.ID, 10), map[string]any{
				"source_uid":      step.SourceUID,
				"revision_id":     step.RevisionID,
				"occurrence_key":  step.OccurrenceKey,
				"trigger":         string(step.Trigger),
				"workflow_run_id": workflowRunID,
			})
	})
	if err != nil {
		return workflow.StepRun{}, false, err
	}
	return stored, created, nil
}

// ---------------------------------------------------------------------------
// Phase
// ---------------------------------------------------------------------------

// updateWorkflowPhaseSQL advances a workflow run's phase and merges the
// decision trail in one guarded write.
//
// The row is locked before it is written so the phase a caller saw is the
// phase the write is compared against. The merge is jsonb concatenation per
// task name -- prior || incoming -- never a wholesale replace, so two ticks
// deciding different tasks compose and a re-decided task states its latest
// verdict. The write fires only when it would change something (the phase
// moves, or the merge adds a decision), and never on a terminal run: terminal
// workflow bookkeeping fires once, like the job run's, and an unguarded write
// would let a late observer drag a finished workflow back into the lifecycle.
const updateWorkflowPhaseSQL = `
WITH prior AS (
  SELECT phase AS prior_phase, task_decisions AS prior_decisions, actor AS run_actor
  FROM workflow_run_control_plane
  WHERE id=$1 AND tenant_id=$2
  FOR UPDATE
)
UPDATE workflow_run_control_plane w
SET phase=$3,
    task_decisions = prior.prior_decisions || $4::jsonb,
    updated_at=$5
FROM prior
WHERE w.id=$1 AND w.tenant_id=$2
  AND NOT (w.phase = ANY($6))
  AND (w.phase <> $3 OR w.task_decisions <> (w.task_decisions || $4::jsonb))
RETURNING w.phase, prior.prior_phase, prior.prior_decisions::text, prior.run_actor`

const selectWorkflowPhaseSQL = `
SELECT phase, task_decisions::text FROM workflow_run_control_plane
WHERE id=$1 AND tenant_id=$2`

// UpdatePhase advances a workflow run's phase and merges the decision trail,
// reporting whether the stored run changed. A decisions-only write (the phase
// held, a task gained its skip verdict) counts as changed: the column is the
// run's record of why those tasks have no step rows. The audit trail records
// phase transitions; the decisions column is its own record and needs no
// second copy.
//
// Refusals: a run that does not exist or belongs to another tenant is
// NotFoundError; a run that already reached a terminal phase is
// ConflictError, because the caller's view of the run conflicts with the
// stored final state. Re-reporting the exact stored state is not a refusal at
// all -- the same calm no-op the job run's phase write returns.
func (r *WorkflowRunRepository) UpdatePhase(
	ctx context.Context, tenantID string, runID int64, phase workflow.Phase, decisions map[string]workflow.TaskDecision,
) (bool, error) {
	if phase == "" {
		return false, fmt.Errorf("phase is required")
	}
	incoming, err := encodeTaskDecisions(decisions)
	if err != nil {
		return false, err
	}

	changed := false
	err = r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var (
			newPhase     string
			priorPhase   string
			priorRawText string
			runActor     string
		)
		scanErr := tx.QueryRowContext(ctx, updateWorkflowPhaseSQL,
			runID, tenantID, string(phase), incoming, r.now().UTC(), terminalWorkflowPhases(),
		).Scan(&newPhase, &priorPhase, &priorRawText, &runActor)
		if scanErr == nil {
			// The statement's predicate guarantees a returned row moved the
			// phase or added a decision; recomputing that here keeps the Go
			// answer tied to the stored trail rather than to the promise.
			prior, decodeErr := decodeTaskDecisions([]byte(priorRawText))
			if decodeErr != nil {
				return decodeErr
			}
			changed = priorPhase != newPhase || decisionsWouldChange(prior, decisions)
			if priorPhase == newPhase {
				return nil
			}
			return insertControlPlaneAudit(ctx, tx, tenantID, runActor, auditEventWorkflowRunPhaseChanged,
				auditResourceWorkflowRun, strconv.FormatInt(runID, 10), map[string]any{
					"from_phase": priorPhase,
					"to_phase":   newPhase,
				})
		}
		if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}

		// The write was refused. Read why: a run that already holds the state
		// the caller asked for is done, a terminal run is final, a missing run
		// is not this tenant's.
		var (
			current   string
			storedRaw string
		)
		readErr := tx.QueryRowContext(ctx, selectWorkflowPhaseSQL, runID, tenantID).
			Scan(&current, &storedRaw)
		if errors.Is(readErr, sql.ErrNoRows) {
			return &resource.NotFoundError{Resource: auditResourceWorkflowRun, ID: runID}
		}
		if readErr != nil {
			return readErr
		}
		stored, decodeErr := decodeTaskDecisions([]byte(storedRaw))
		if decodeErr != nil {
			return decodeErr
		}
		if current == string(phase) && !decisionsWouldChange(stored, decisions) {
			return nil
		}
		if workflow.Terminal(workflow.Phase(current)) {
			return &resource.ConflictError{
				Resource: auditResourceWorkflowRun,
				ID:       runID,
				Message: fmt.Sprintf("workflow run %d is %s and final: its phase can no longer change",
					runID, current),
			}
		}
		return fmt.Errorf("workflow run %d phase write matched no row while in phase %s", runID, current)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// ---------------------------------------------------------------------------
// Retention
// ---------------------------------------------------------------------------

// PrunableWorkflowRun is one workflow run beyond the retained history, listed
// for the dedicated workflow prune. The phase set it was selected from is
// workflowRetentionPhases.
type PrunableWorkflowRun struct {
	ID            int64
	OccurrenceKey string
	Phase         workflow.Phase
}

// prunableWorkflowRunsSQL ranks workflow runs per outcome in one pass, the
// job-run ledger's prunableRunsSQL shape over the workflow table: the kept
// history is decided by the database and the two keep limits can differ
// without the caller issuing a query per outcome.
const prunableWorkflowRunsSQL = `
SELECT id, occurrence_key, phase FROM (
  SELECT id, occurrence_key, phase,
         row_number() OVER (PARTITION BY phase ORDER BY created_at DESC, id DESC) AS rank,
         CASE WHEN phase = $4 THEN $5::int ELSE $6::int END AS keep
  FROM workflow_run_control_plane
  WHERE tenant_id=$1 AND source_uid=$2 AND phase = ANY($3)
) ranked
WHERE rank > keep
ORDER BY id`

// PrunableRuns lists workflow runs of one WorkflowJob beyond the retained
// history. Rows are ranked per outcome, so a burst of failures cannot evict
// the success history and vice versa. Deleting one is PruneRun's job, never a
// bare DELETE: the steps-then-row order is the only order the schema allows.
func (r *WorkflowRunRepository) PrunableRuns(
	ctx context.Context, tenantID, sourceUID string, keepSuccessful, keepFailed int,
) ([]PrunableWorkflowRun, error) {
	if keepSuccessful < 0 {
		keepSuccessful = 0
	}
	if keepFailed < 0 {
		keepFailed = 0
	}
	var out []PrunableWorkflowRun
	err := r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, prunableWorkflowRunsSQL,
			tenantID, sourceUID, workflowRetentionPhases(), string(workflow.PhaseSucceeded),
			keepSuccessful, keepFailed)
		if queryErr != nil {
			return queryErr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				run   PrunableWorkflowRun
				phase string
			)
			if scanErr := rows.Scan(&run.ID, &run.OccurrenceKey, &phase); scanErr != nil {
				return scanErr
			}
			run.Phase = workflow.Phase(phase)
			out = append(out, run)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const pruneWorkflowStepsSQL = `
DELETE FROM job_run_control_plane
WHERE workflow_run_id=$1 AND tenant_id=$2
RETURNING id, occurrence_key, phase, actor`

const pruneWorkflowRowSQL = `
DELETE FROM workflow_run_control_plane
WHERE id=$1 AND tenant_id=$2 AND phase = ANY($3)
RETURNING occurrence_key, phase, actor`

// PruneRun removes one workflow run and its step runs atomically: the step
// rows delete first, the workflow row deletes only after its last step is
// gone, and both deletes commit together or not at all. The FK is RESTRICT,
// so any other order fails loudly instead of orphaning steps -- that
// loudness is the contract, and this method exists so callers never learn
// the hard way.
//
// Eligibility is re-checked here rather than trusted from the caller's
// listing: the row is locked FOR UPDATE and the row delete repeats the phase
// predicate, so a run that left the retention set between the scan and the
// prune is left alone rather than silently dropped mid-flight. A workflow run
// with no steps and an ineligible phase reports not deleted, the same calm
// answer the job run's DeleteRun gives a row it did not touch.
func (r *WorkflowRunRepository) PruneRun(
	ctx context.Context, tenantID string, runID int64,
) (deleted bool, stepsDeleted int64, err error) {
	err = r.withTenantTx(ctx, tenantID, func(ctx context.Context, tx *sql.Tx) error {
		var (
			occurrenceKey string
			phase         string
			actor         string
		)
		lockErr := tx.QueryRowContext(ctx, `
			SELECT occurrence_key, phase, actor FROM workflow_run_control_plane
			WHERE id=$1 AND tenant_id=$2
			FOR UPDATE`, runID, tenantID,
		).Scan(&occurrenceKey, &phase, &actor)
		if errors.Is(lockErr, sql.ErrNoRows) {
			// Nothing to prune, or not this tenant's -- retention will not see
			// it again either way.
			return nil
		}
		if lockErr != nil {
			return lockErr
		}
		if !workflowRetentionPhasesContains(phase) {
			return nil
		}

		// Steps first. The delete takes the attempts rows with it (the
		// attempt FK is CASCADE), and each removed step is attributed before
		// the workflow row that grouped it goes away.
		stepRows, queryErr := tx.QueryContext(ctx, pruneWorkflowStepsSQL, runID, tenantID)
		if queryErr != nil {
			return queryErr
		}
		var steps int64
		for stepRows.Next() {
			var (
				stepID    int64
				stepKey   string
				stepPhase string
				stepActor string
			)
			if scanErr := stepRows.Scan(&stepID, &stepKey, &stepPhase, &stepActor); scanErr != nil {
				_ = stepRows.Close()
				return scanErr
			}
			if auditErr := insertControlPlaneAudit(ctx, tx, tenantID, stepActor, auditEventRunPruned,
				auditResourceRun, strconv.FormatInt(stepID, 10), map[string]any{
					"occurrence_key":  stepKey,
					"phase":           stepPhase,
					"workflow_run_id": runID,
				}); auditErr != nil {
				_ = stepRows.Close()
				return auditErr
			}
			steps++
		}
		if err := stepRows.Err(); err != nil {
			_ = stepRows.Close()
			return err
		}
		// The rows are drained; the next statement in this transaction needs
		// the connection back before it can run.
		_ = stepRows.Close()

		// Then the row. We hold its lock and re-checked the phase above, so a
		// miss here means the row moved underneath the retention set's feet --
		// refuse loudly and let the rollback take the step deletes with it,
		// rather than report a prune that did not happen.
		var rowKey, rowPhase, rowActor string
		deleteErr := tx.QueryRowContext(ctx, pruneWorkflowRowSQL,
			runID, tenantID, workflowRetentionPhases(),
		).Scan(&rowKey, &rowPhase, &rowActor)
		if errors.Is(deleteErr, sql.ErrNoRows) {
			return fmt.Errorf("workflow run %d became unprunable between the eligibility check and its delete", runID)
		}
		if deleteErr != nil {
			return deleteErr
		}

		if err := insertControlPlaneAudit(ctx, tx, tenantID, rowActor, auditEventWorkflowRunPruned,
			auditResourceWorkflowRun, strconv.FormatInt(runID, 10), map[string]any{
				"occurrence_key": rowKey,
				"phase":          rowPhase,
				"steps_deleted":  steps,
			}); err != nil {
			return err
		}
		deleted = true
		stepsDeleted = steps
		return nil
	})
	if err != nil {
		return false, 0, err
	}
	return deleted, stepsDeleted, nil
}

// workflowRetentionPhasesContains is the Go-side membership test for the
// retention phase set, so the eligibility read and the SQL predicate in
// pruneWorkflowRowSQL answer from one list.
func workflowRetentionPhasesContains(phase string) bool {
	for _, eligible := range workflowRetentionPhases() {
		if eligible == phase {
			return true
		}
	}
	return false
}
