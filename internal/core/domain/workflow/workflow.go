// Package workflow is the pure domain view of a WorkflowJob execution: the
// DAG a pinned revision decodes to, the condition grammar a task's execution
// is gated by, and the derivation of a workflow run's phase from its steps'
// ledger facts. It holds no I/O and no Kubernetes types beyond the CR spec it
// converts from; the walker, the projection boundary and any validator share
// these one shapes rather than each declaring structurally identical types
// that will not interchange.
package workflow

import (
	"time"

	"orbitjob/internal/core/domain/jobrun"
)

// SourceModeWorkflow is the job_definition_revisions source_mode a WorkflowJob
// materializes under. Revisions are unique per (source_mode, source_uid,
// generation), so workflow-sourced revisions live in their own namespace and
// can never collide with kubernetes, check or function rows.
const SourceModeWorkflow = "workflow"

// Phase is the workflow-level lifecycle of one workflow execution. It is the
// ledger column's vocabulary, not the step lifecycle: a workflow run is one
// row in workflow_run_control_plane and its steps are ordinary job runs.
type Phase string

const (
	PhasePending         Phase = "Pending"
	PhaseRunning         Phase = "Running"
	PhaseCancelRequested Phase = "CancelRequested"
	PhaseSucceeded       Phase = "Succeeded"
	PhaseFailed          Phase = "Failed"
	PhaseCanceled        Phase = "Canceled"
	PhaseCancelUnknown   Phase = "CancelUnknown"
)

// Terminal reports whether a workflow phase is final. CancelUnknown is
// deliberately excluded, mirroring jobrun.Terminal: a stop the platform could
// not confirm is not an outcome, and a workflow whose step sits in
// CancelUnknown stays non-terminal so nothing downstream treats it as done.
func Terminal(p Phase) bool {
	return p == PhaseSucceeded || p == PhaseFailed || p == PhaseCanceled
}

// FailPolicy decides what a terminally failed step means for the rest of the
// workflow. FailFast stops creating steps; in-flight steps are respected, not
// preempted. Continue lets creation proceed per dependsOn and conditions.
type FailPolicy string

const (
	FailPolicyFailFast FailPolicy = "FailFast"
	FailPolicyContinue FailPolicy = "Continue"
)

// Skip reasons recorded in a workflow run's task decisions. A skipped task has
// no ledger row and no phase; the decision trail is the only record of why.
const (
	// ReasonCanceled: cancellation began before the task was created.
	ReasonCanceled = "canceled"
	// ReasonFailFast: the workflow's fail policy stopped new work.
	ReasonFailFast = "fail_fast"
	// ReasonCondition: the task's condition failed against terminal upstream
	// state.
	ReasonCondition = "condition"
	// ReasonDeadline: the workflow-level deadline passed before the task was
	// created.
	ReasonDeadline = "deadline"
)

// TaskDecision is one task's skip verdict, stored in the workflow run row's
// task_decisions JSONB keyed by task name. It is the workflow's own short
// annotation of why a task produced no run; the authoritative facts about the
// tasks that did run are their step rows.
type TaskDecision struct {
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason,omitempty"`
}

// Run is one workflow execution: one row in workflow_run_control_plane, one
// occurrence of a pinned workflow revision. Its steps are ordinary job runs
// grouped by WorkflowRunID.
type Run struct {
	ID            int64
	TenantID      string
	SourceUID     string
	RevisionID    int64
	OccurrenceKey string
	// Trigger is why the workflow run exists: Schedule for a cron firing,
	// Manual for a run materialized from a WorkflowRun CR. The column is
	// free text exactly like job_run_control_plane.trigger; these are the
	// values v1 writes.
	Trigger jobrun.Trigger
	Actor   string
	Phase   Phase
	// TaskDecisions maps task name to skip verdict for every task the walker
	// decided not to create. Empty when every created task got a run.
	TaskDecisions map[string]TaskDecision
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StepRun is the workflow view of one job_run_control_plane row whose
// workflow_run_id groups it under a run. The ledger row does not carry the
// task name; the walker maps rows back to tasks by re-deriving each task's
// step occurrence key, which is why the derivation lives in this package.
type StepRun struct {
	ID            int64
	WorkflowRunID int64
	// SourceUID is the referenced ScheduledJob definition's identity, not the
	// workflow's: a step is an ordinary run of that definition.
	SourceUID     string
	RevisionID    int64
	OccurrenceKey string
	Trigger       jobrun.Trigger
	Phase         jobrun.Phase
	Attempt       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StepState is the ledger-visible fact set of one task's step, the operand
// set conditions evaluate over. Evaluation happens once, when all of the
// depending task's dependencies are terminal, so a state is always terminal
// when it is consulted.
type StepState struct {
	Phase      jobrun.Phase
	Attempt    int
	DurationMs int64
}

// DAG is the walker's view of a pinned workflow revision. Identity (source
// uid, revision id, schedule) stays on the revision row the walker already
// holds; the DAG carries only what advancing a run needs.
type DAG struct {
	// FailPolicy decides what a failed step means for the rest of the run.
	FailPolicy FailPolicy
	// TimeoutSeconds is the workflow-level wall-clock deadline,
	// walker-enforced and honestly weaker than a task's own deadline, which
	// Kubernetes enforces. Zero means no workflow-level deadline.
	TimeoutSeconds int
	Tasks          []Task
}

// Task is one node of the DAG: a reference to a ScheduledJob definition, the
// tasks that must finish first, and the optional condition gating execution.
type Task struct {
	Name string
	// JobRef is the referenced ScheduledJob definition's name in the
	// workflow's namespace. The step runs that definition's active revision
	// with that definition's own timeout and retry budget.
	JobRef    string
	DependsOn []string
	Condition *Condition
}
