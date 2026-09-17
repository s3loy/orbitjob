package postgres

import (
	"context"

	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/workflow"
)

// This file is the contract layer for the Functions and Workflows ledger and
// configuration surfaces: the interfaces implementations must satisfy, kept
// beside the repositories they will be implemented against. There are no
// implementations here on purpose — the storage work lands against these
// signatures, not ahead of them.
//
// Every method is tenant-scoped by explicit argument, matching the house
// rule that tenancy is an argument, never ambient state: implementations run
// inside the tenant GUC (WithTenantTransaction) and the RLS policies are the
// second gate, not the first.

// WorkflowRunStore owns the workflow_run_control_plane rows: one workflow
// execution per (source_uid, occurrence_key), its skip-decision trail, and
// the step runs grouped under it by job_run_control_plane.workflow_run_id.
// The workflow row is the state; the column is the pointer. Implementations
// set updated_at in SQL — the ledger family carries no set_updated_at
// trigger.
type WorkflowRunStore interface {
	// CreateRunForTenant inserts one workflow run for the pinned revision,
	// deduplicating on (source_uid, occurrence_key). created is false when
	// the occurrence already exists, in which case the stored row is
	// returned unchanged — firing the same occurrence twice converges on one
	// row, never two.
	CreateRunForTenant(ctx context.Context, tenantID string, run workflow.Run) (stored workflow.Run, created bool, err error)
	// RunByOccurrence loads one run by its dedup identity.
	RunByOccurrence(ctx context.Context, tenantID, sourceUID, occurrenceKey string) (workflow.Run, bool, error)
	// RunByID loads one run by ledger id. found is false for a run that does
	// not exist or belongs to another tenant.
	RunByID(ctx context.Context, tenantID string, id int64) (workflow.Run, bool, error)
	// OpenRuns lists the tenant's non-terminal workflow runs — the advance
	// pass's input. A run at CancelRequested is open: its steps still need
	// observing even though no new step may be created.
	OpenRuns(ctx context.Context, tenantID string) ([]workflow.Run, error)
	// Steps lists the step runs grouped under one workflow run, ordered by id
	// so a walker's decisions are reproducible across ticks.
	Steps(ctx context.Context, tenantID string, workflowRunID int64) ([]workflow.StepRun, error)
	// CreateStepForTenant inserts one step run of the referenced definition
	// and groups it under the workflow run in the same transaction: the same
	// row shape, dedup and attempt bookkeeping the job run occurrence insert
	// performs, plus the workflow_run_id pointer. created is false when the
	// step's occurrence key already exists for the referenced definition.
	CreateStepForTenant(ctx context.Context, tenantID string, workflowRunID int64, step workflow.StepRun, maxAttempts int, actor string) (stored workflow.StepRun, created bool, err error)
	// UpdatePhase advances a workflow run's phase and merges the decision
	// trail: decisions are merged into the stored task_decisions (jsonb
	// concatenation per task name), never a wholesale replace, so two ticks
	// deciding different tasks compose. The write is refused for a run that
	// already reached a terminal phase and changed is false in that case —
	// terminal workflow bookkeeping fires once, like the job run's.
	UpdatePhase(ctx context.Context, tenantID string, runID int64, phase workflow.Phase, decisions map[string]workflow.TaskDecision) (changed bool, err error)
}

// FunctionStore is the configuration surface for function definitions: the
// admin API's CRUD reads and writes, and the operator's revision-sync loop
// read. functions is configuration family — a tenant's functions die with the
// tenant, the admin role holds the DML, and the runtime role is SELECT-only
// because nothing on the execution side writes a definition.
type FunctionStore interface {
	// CreateForTenant inserts one function definition and returns it with its
	// assigned id and version.
	CreateForTenant(ctx context.Context, tenantID string, def function.Definition) (function.Definition, error)
	// UpdateForTenant saves a new version of the definition: implementations
	// bump version and keep the active revision pointer honest in the same
	// transaction, refusing a write against a stale Version.
	UpdateForTenant(ctx context.Context, tenantID string, def function.Definition) (function.Definition, error)
	// GetForTenant loads one live (non-deleted) definition. found is false
	// when it does not exist, is deleted, or belongs to another tenant.
	GetForTenant(ctx context.Context, tenantID string, id int64) (function.Definition, bool, error)
	// ListForTenant lists the tenant's live definitions.
	ListForTenant(ctx context.Context, tenantID string) ([]function.Definition, error)
	// ActiveForTenant lists the tenant's active, live definitions — the
	// operator sync loop's input. The loop materializes one revision per
	// definition version, idempotently, so the steady-state read is this
	// list once per tick.
	ActiveForTenant(ctx context.Context, tenantID string) ([]function.Definition, error)
	// DeleteForTenant soft-deletes the definition. Invocation history in
	// function_runs outlives it: soft delete keeps the row, so the read
	// model's tenant-matched foreign key never dangles.
	DeleteForTenant(ctx context.Context, tenantID string, id int64) error
}

// FunctionRunStore maintains the function_runs read model: one row per
// terminal function invocation, written by the operator's terminal-phase
// bookkeeping (the hook that also feeds the check read model and the SLI
// snapshots) and read by the admin API. There is no pending or running state
// — live progress lives on the JobRun CR and the ledger row, not here.
type FunctionRunStore interface {
	// RecordCompleted upserts one terminal invocation. RunID is the
	// deterministic UUID derived from the ledger occurrence key, so a
	// replayed recording lands on the same row; the insert is a no-op on
	// conflict. Status is the exit outcome (success, failed, canceled) —
	// never a non-terminal phase.
	RecordCompleted(ctx context.Context, tenantID string, record function.CompletedRecord) error
	// RunsByFunction lists the function's most recent terminal invocations,
	// newest first, for the admin read surface. A non-positive limit means
	// the implementation's default page.
	RunsByFunction(ctx context.Context, tenantID string, functionID int64, limit int) ([]function.FunctionRun, error)
}
