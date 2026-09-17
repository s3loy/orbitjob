package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ScheduledJobSpec struct {
	Schedule string `json:"schedule"`
	// History limits how many terminal runs are retained, per outcome.
	History           HistoryPolicy     `json:"history,omitempty"`
	Suspend           bool              `json:"suspend,omitempty"`
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
	MisfirePolicy     MisfirePolicy     `json:"misfirePolicy,omitempty"`
	// TimeoutSeconds is the default deadline for every run of this definition.
	// The scheduler copies it into each JobRun so a run's own spec stays
	// authoritative even after the definition is edited.
	TimeoutSeconds int32           `json:"timeoutSeconds,omitempty"`
	JobTemplate    JobTemplateSpec `json:"jobTemplate"`
	RetryPolicy    RetryPolicy     `json:"retryPolicy,omitempty"`
}
type JobTemplateSpec struct {
	Image        string   `json:"image"`
	Command      []string `json:"command,omitempty"`
	Args         []string `json:"args,omitempty"`
	BackoffLimit int32    `json:"backoffLimit,omitempty"`
}
type RetryPolicy struct {
	MaxAttempts int32 `json:"maxAttempts,omitempty"`
}

// EffectiveMaxAttempts resolves the retry budget a run inherits from the
// definition that created it. An unset or non-positive value means one attempt,
// not unlimited: the run row's max_attempts column requires at least one, and a
// zero that meant "retry forever" would be a runaway no policy asked for. Both
// the scheduler and the operator resolve the budget here so a manual run and a
// scheduled run of one definition retry the same number of times.
func (p RetryPolicy) EffectiveMaxAttempts() int {
	if p.MaxAttempts < 1 {
		return 1
	}
	return int(p.MaxAttempts)
}

// HistoryPolicy bounds retained runs. Zero means the default, not unlimited:
// an unbounded history would grow both the API server and the database forever.
type HistoryPolicy struct {
	SuccessfulRuns int32 `json:"successfulRuns,omitempty"`
	FailedRuns     int32 `json:"failedRuns,omitempty"`
}
type ConcurrencyPolicy string

const (
	Allow   ConcurrencyPolicy = "Allow"
	Forbid  ConcurrencyPolicy = "Forbid"
	Replace ConcurrencyPolicy = "Replace"
)

type MisfirePolicy string

const (
	Skip           MisfirePolicy = "Skip"
	FireOnce       MisfirePolicy = "FireOnce"
	CatchUpBounded MisfirePolicy = "CatchUpBounded"
)

type JobRunSpec struct {
	ScheduledJobRef    ObjectReference `json:"scheduledJobRef"`
	DefinitionRevision int64           `json:"definitionRevision"`
	Trigger            Trigger         `json:"trigger"`
	// Actor is who asked for this run: the caller of a manual trigger, or the
	// scheduler's own name for an occurrence no person triggered. It is a spec
	// field, not the annotation the API layer first used, because the ledger's
	// central claim is "who triggered this" and the CRD is the only place that
	// claim can be validated. The schema makes the field required and non-empty,
	// so a JobRun without an actor cannot be admitted; an annotation is free
	// text the schema cannot reject, and it can be dropped by a client that does
	// not know to carry it. A writer with CR access can edit either, so the
	// protection is RBAC on the resource, not the field's location; the field
	// earns its place by being part of the declared intent the operator copies
	// into job_run_control_plane.actor. The operator trusts the object it reads:
	// it cannot tell an API-written JobRun from a hand-written one, so the actor
	// is exactly as trustworthy as the RBAC that governs who may write a
	// JobRun. The admin API holds create, get and patch, and the patch exists
	// only to set spec.cancelRequested on a cancel request. A mutating
	// admission webhook stamping this field from request.userInfo.username is
	// the only mechanism that would make it unforgeable regardless of the
	// writer.
	Actor         string `json:"actor"`
	OccurrenceKey string `json:"occurrenceKey"`
	// TimeoutSeconds bounds this run. The platform sets it on the Kubernetes
	// Job as activeDeadlineSeconds, so enforcement survives an operator outage.
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
	// CancelRequested is the user's stop intent. It is a request, not a fact:
	// the run only reaches Canceled once the Kubernetes Job is observed gone.
	CancelRequested bool `json:"cancelRequested,omitempty"`
}
type Trigger string

const (
	Schedule Trigger = "Schedule"
	Manual   Trigger = "Manual"
	Retry    Trigger = "Retry"
	// Check marks a run fired by the check scheduler for a probe-style check
	// definition. It is distinct from Schedule so ledger readers can select
	// check traffic without parsing source_uid strings; a Check CR is still
	// refused unless the check scheduler already committed its run row.
	Check Trigger = "Check"
	// Workflow marks a run that is one step of a workflow execution: an
	// ordinary run of the referenced ScheduledJob definition, created by the
	// operator's workflow walker and grouped to its workflow run by the
	// ledger's workflow_run_id column. It is distinct from Schedule because a
	// step is neither a cron occurrence of the task's own schedule nor a
	// person's request, and ledger readers select or exclude workflow traffic
	// without parsing source_uid pairs. A Workflow step CR is refused unless
	// the walker already committed its step row.
	Workflow Trigger = "Workflow"
	// Function marks a run fired by an HTTP function invocation. It is
	// CR-first exactly like Manual: the API publishes the JobRun and the
	// operator materializes the ledger row from the pinned function revision.
	// It is distinct from Manual so ledger readers can answer "how often was
	// this function invoked" without parsing occurrence keys.
	Function Trigger = "Function"
)

type ObjectReference struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}
type Condition struct {
	Type               string       `json:"type"`
	Status             string       `json:"status"`
	Reason             string       `json:"reason,omitempty"`
	Message            string       `json:"message,omitempty"`
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}
type JobRunStatus struct {
	Phase            string           `json:"phase,omitempty"`
	Conditions       []Condition      `json:"conditions,omitempty"`
	KubernetesJobRef *ObjectReference `json:"kubernetesJobRef,omitempty"`
	Attempt          int32            `json:"attempt,omitempty"`
	StartedAt        *metav1.Time     `json:"startedAt,omitempty"`
	CompletedAt      *metav1.Time     `json:"completedAt,omitempty"`
}
type ScheduledJobStatus struct {
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
	ActiveRevision     int64        `json:"activeRevision,omitempty"`
	Conditions         []Condition  `json:"conditions,omitempty"`
	LastScheduleTime   *metav1.Time `json:"lastScheduleTime,omitempty"`
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`
}

// WorkflowJobStatus mirrors ScheduledJobStatus: the projection reports the
// active revision it materialized, or a condition explaining why it refused
// (an invalid DAG, an unresolvable task reference).
type WorkflowJobStatus struct {
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
	ActiveRevision     int64        `json:"activeRevision,omitempty"`
	Conditions         []Condition  `json:"conditions,omitempty"`
	LastScheduleTime   *metav1.Time `json:"lastScheduleTime,omitempty"`
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`
}

func ReplaceCondition(conditions []Condition, next Condition) []Condition {
	out := make([]Condition, 0, len(conditions)+1)
	replaced := false
	for _, c := range conditions {
		if c.Type == next.Type {
			out = append(out, next)
			replaced = true
		} else {
			out = append(out, c)
		}
	}
	if !replaced {
		out = append(out, next)
	}
	return out
}

// WorkflowFailPolicy decides what a failed step means for the rest of the
// workflow. FailFast stops new work immediately once a step fails terminally;
// in-flight steps are respected, not preempted. Continue lets task creation
// proceed per dependsOn and conditions alone, so a failed upstream only blocks
// tasks that actually require it.
type WorkflowFailPolicy string

const (
	FailFast WorkflowFailPolicy = "FailFast"
	Continue WorkflowFailPolicy = "Continue"
)

// WorkflowConditionType selects how a task's rules combine.
type WorkflowConditionType string

const (
	// ConditionAll passes only when every rule passes.
	ConditionAll WorkflowConditionType = "all"
	// ConditionAny passes when at least one rule passes.
	ConditionAny WorkflowConditionType = "any"
)

// WorkflowRuleField names the ledger-visible fact a condition rule reads.
// The set is closed on purpose: these are the fields job_run_control_plane and
// its attempts can actually answer, so evaluation stays a pure function of
// stored rows.
type WorkflowRuleField string

const (
	// RuleFieldPhase reads the step run's lifecycle phase (Succeeded, Failed,
	// Canceled are the values that matter post-terminal).
	RuleFieldPhase WorkflowRuleField = "phase"
	// RuleFieldAttempts reads how many attempts the upstream step spent.
	RuleFieldAttempts WorkflowRuleField = "attempts"
	// RuleFieldDurationMs reads the terminal attempt's wall-clock duration in
	// milliseconds.
	RuleFieldDurationMs WorkflowRuleField = "duration_ms"
)

// WorkflowRuleOperator is the comparison a rule applies. String operators
// (eq, ne, in, not_in) compare phase names; numeric operators (lt, le, gt, ge)
// compare attempt counts and durations.
type WorkflowRuleOperator string

const (
	OperatorEq    WorkflowRuleOperator = "eq"
	OperatorNe    WorkflowRuleOperator = "ne"
	OperatorIn    WorkflowRuleOperator = "in"
	OperatorNotIn WorkflowRuleOperator = "not_in"
	OperatorLt    WorkflowRuleOperator = "lt"
	OperatorLe    WorkflowRuleOperator = "le"
	OperatorGt    WorkflowRuleOperator = "gt"
	OperatorGe    WorkflowRuleOperator = "ge"
)

// WorkflowRule is one comparison against one upstream task's ledger facts.
type WorkflowRule struct {
	// Task names the upstream task whose facts the rule reads. It must appear
	// in the depending task's dependsOn, so a rule can never reference state
	// that does not exist yet or create hidden ordering.
	Task     string               `json:"task"`
	Field    WorkflowRuleField    `json:"field"`
	Operator WorkflowRuleOperator `json:"operator"`
	// Values carries the comparands of the string operators: exactly one entry
	// for eq and ne, one or more for in and not_in, empty for the numeric
	// operators.
	Values []string `json:"values,omitempty"`
	// Number carries the comparand of the numeric operators. It is a pointer
	// so a zero bound survives serialization as present rather than absent.
	Number *int64 `json:"number,omitempty"`
}

// WorkflowConditionSpec decides whether a task executes once its dependencies
// are terminal. A nil condition means unconditional execution.
type WorkflowConditionSpec struct {
	Type  WorkflowConditionType `json:"type"`
	Rules []WorkflowRule        `json:"rules"`
}

// WorkflowTaskSpec is one node of the workflow DAG. A task does not carry its
// own image, timeout or retry budget: it names an existing ScheduledJob
// definition in the same namespace, and every step of it runs that
// definition's active revision with that definition's own budgets. Workflow
// fields that would not be enforced by anything are deliberately absent.
type WorkflowTaskSpec struct {
	// Name identifies the task inside this workflow. It is the key conditions,
	// dependency edges and the step occurrence keys are built from, so names
	// must be unique within the spec.
	Name string `json:"name"`
	// JobRef names the ScheduledJob definition this task runs.
	JobRef WorkflowTaskJobRef `json:"jobRef"`
	// DependsOn lists the tasks that must reach a terminal phase before this
	// task is eligible.
	DependsOn []string `json:"dependsOn,omitempty"`
	// Condition, when set, is evaluated once all DependsOn tasks are terminal;
	// a failed condition skips the task without creating any run.
	Condition *WorkflowConditionSpec `json:"condition,omitempty"`
}

// WorkflowTaskJobRef names the ScheduledJob definition a task executes.
type WorkflowTaskJobRef struct {
	Name string `json:"name"`
}

// WorkflowJobSpec declares a workflow: a schedule that fires workflow runs and
// a DAG of tasks, each an ordinary run of a referenced ScheduledJob. The
// workflow-level timeout is walker-enforced and honestly weaker than a task
// deadline (which Kubernetes enforces as activeDeadlineSeconds): an operator
// outage past the deadline goes unpunished.
type WorkflowJobSpec struct {
	Schedule string `json:"schedule"`
	// History bounds retained workflow runs per outcome. Zero means the
	// platform default, not unlimited, matching HistoryPolicy.
	History           HistoryPolicy     `json:"history,omitempty"`
	Suspend           bool              `json:"suspend,omitempty"`
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
	// TimeoutSeconds is the workflow-level wall-clock deadline. Replace is
	// deliberately not a workflow concurrency mode: replacing a DAG means
	// cancel plus a new run, not a policy.
	TimeoutSeconds int32              `json:"timeoutSeconds,omitempty"`
	FailPolicy     WorkflowFailPolicy `json:"failPolicy,omitempty"`
	Tasks          []WorkflowTaskSpec `json:"tasks"`
}

// Validate checks the spec fields the CR shape can answer on its own: a
// schedule, a non-empty task set with unique names, dependency references that
// resolve, and conditions that only read declared dependencies with a closed
// field and operator grammar. Cycle detection is deliberately not here: it is
// the projection boundary's DAG validation (domain workflow ValidateDAG), which
// runs when the CR is reconciled, the same split ScheduledJob uses (structural
// typing from the CRD, semantic checks from the operator).
func (s WorkflowJobSpec) Validate() error {
	if s.Schedule == "" {
		return fmt.Errorf("workflowJobSpec.schedule is required")
	}
	if len(s.Tasks) == 0 {
		return fmt.Errorf("workflowJobSpec.tasks must have at least one task")
	}
	names := make(map[string]bool, len(s.Tasks))
	declared := make(map[string]WorkflowTaskSpec, len(s.Tasks))
	for _, task := range s.Tasks {
		if task.Name == "" {
			return fmt.Errorf("workflowJobSpec.tasks: task name must not be empty")
		}
		if names[task.Name] {
			return fmt.Errorf("workflowJobSpec.tasks: duplicate task name %q", task.Name)
		}
		names[task.Name] = true
		declared[task.Name] = task
		if task.JobRef.Name == "" {
			return fmt.Errorf("workflowJobSpec.tasks[%s].jobRef.name is required", task.Name)
		}
	}
	for _, task := range s.Tasks {
		seenDep := make(map[string]bool, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if dep == task.Name {
				return fmt.Errorf("workflowJobSpec.tasks[%s]: depends on itself", task.Name)
			}
			if !names[dep] {
				return fmt.Errorf("workflowJobSpec.tasks[%s]: depends on unknown task %q", task.Name, dep)
			}
			if seenDep[dep] {
				return fmt.Errorf("workflowJobSpec.tasks[%s]: depends on %q twice", task.Name, dep)
			}
			seenDep[dep] = true
		}
		if task.Condition == nil {
			continue
		}
		if err := validateCondition(task.Condition, task.DependsOn); err != nil {
			return fmt.Errorf("workflowJobSpec.tasks[%s]: %w", task.Name, err)
		}
	}
	return nil
}

func validateCondition(cond *WorkflowConditionSpec, dependsOn []string) error {
	if cond.Type != ConditionAll && cond.Type != ConditionAny {
		return fmt.Errorf("condition.type must be %q or %q", ConditionAll, ConditionAny)
	}
	if len(cond.Rules) == 0 {
		return fmt.Errorf("condition.rules must have at least one rule")
	}
	dependencies := make(map[string]bool, len(dependsOn))
	for _, dep := range dependsOn {
		dependencies[dep] = true
	}
	for _, rule := range cond.Rules {
		if rule.Task == "" {
			return fmt.Errorf("condition.rules: task must not be empty")
		}
		if !dependencies[rule.Task] {
			return fmt.Errorf("condition.rules: task %q is not in the owning task's dependsOn", rule.Task)
		}
		if rule.Field != RuleFieldPhase && rule.Field != RuleFieldAttempts && rule.Field != RuleFieldDurationMs {
			return fmt.Errorf("condition.rules: unknown field %q", rule.Field)
		}
		numeric := rule.Operator == OperatorLt || rule.Operator == OperatorLe ||
			rule.Operator == OperatorGt || rule.Operator == OperatorGe
		stringOp := rule.Operator == OperatorEq || rule.Operator == OperatorNe ||
			rule.Operator == OperatorIn || rule.Operator == OperatorNotIn
		if !numeric && !stringOp {
			return fmt.Errorf("condition.rules: unknown operator %q", rule.Operator)
		}
		if numeric {
			if rule.Field == RuleFieldPhase {
				return fmt.Errorf("condition.rules: operator %q does not apply to field %q", rule.Operator, rule.Field)
			}
			if rule.Number == nil {
				return fmt.Errorf("condition.rules: operator %q requires number", rule.Operator)
			}
			if len(rule.Values) != 0 {
				return fmt.Errorf("condition.rules: operator %q takes number, not values", rule.Operator)
			}
			continue
		}
		if rule.Field != RuleFieldPhase {
			return fmt.Errorf("condition.rules: operator %q does not apply to field %q", rule.Operator, rule.Field)
		}
		if rule.Number != nil {
			return fmt.Errorf("condition.rules: operator %q takes values, not number", rule.Operator)
		}
		switch rule.Operator {
		case OperatorEq, OperatorNe:
			if len(rule.Values) != 1 {
				return fmt.Errorf("condition.rules: operator %q requires exactly one value", rule.Operator)
			}
		case OperatorIn, OperatorNotIn:
			if len(rule.Values) < 1 {
				return fmt.Errorf("condition.rules: operator %q requires at least one value", rule.Operator)
			}
		}
	}
	return nil
}

// WorkflowRunStatusStep is the per-step state the operator patches onto a
// WorkflowRun's status from the ledger. It is a read model: a step's
// authoritative facts are its job_run_control_plane row and its JobRun CR.
type WorkflowRunStatusStep struct {
	// Task is the workflow task name the step belongs to.
	Task string `json:"task"`
	// Phase is the step run's lifecycle phase, empty until the step's run row
	// exists.
	Phase   string `json:"phase,omitempty"`
	Attempt int32  `json:"attempt,omitempty"`
	// OccurrenceKey is the step key, from which the step's JobRun object name
	// is derived, so cancel and inspection address the step like any run.
	OccurrenceKey string       `json:"occurrenceKey,omitempty"`
	StartedAt     *metav1.Time `json:"startedAt,omitempty"`
	CompletedAt   *metav1.Time `json:"completedAt,omitempty"`
	// Skipped marks a task that never got a run: its condition failed, its
	// workflow's fail policy stopped new work, the workflow deadline passed,
	// or cancellation began before it was created. SkipReason carries which.
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skipReason,omitempty"`
}

// WorkflowRunStatus is the operator-maintained view of one workflow execution.
type WorkflowRunStatus struct {
	// Phase is the workflow-level lifecycle: Pending, Running,
	// CancelRequested, Succeeded, Failed, Canceled, CancelUnknown. A workflow
	// with a CancelUnknown step stays CancelUnknown and non-terminal, exactly
	// as its step does.
	Phase       string                  `json:"phase,omitempty"`
	Conditions  []Condition             `json:"conditions,omitempty"`
	Steps       []WorkflowRunStatusStep `json:"steps,omitempty"`
	StartedAt   *metav1.Time            `json:"startedAt,omitempty"`
	CompletedAt *metav1.Time            `json:"completedAt,omitempty"`
}

// WorkflowRunSpec publishes one manual workflow execution. It is the
// WorkflowRun trust boundary, mirrored from JobRun: the revision is required
// and pinned before publish, the actor is the authenticated caller rather than
// a client-supplied label, and the occurrence key makes a retried trigger
// resolve to the same run. The operator materializes the workflow run row from
// it and thereafter owns every state transition.
type WorkflowRunSpec struct {
	// WorkflowRef names the WorkflowJob definition this run executes.
	WorkflowRef ObjectReference `json:"workflowRef"`
	// DefinitionRevision is the workflow revision id the run is pinned to. It
	// is required: the operator refuses a run whose revision it cannot load,
	// so a forged or stale reference cannot invent work.
	DefinitionRevision int64 `json:"definitionRevision"`
	// Trigger is why this run exists. The WorkflowRun CR is the manual-trigger
	// transport, and Manual is the only value the schema admits.
	Trigger Trigger `json:"trigger"`
	// Actor is who asked for this run, under the same discipline as JobRun's
	// actor: the caller's credential, schema-required and non-empty, trusted
	// exactly as far as the RBAC that governs who may write a WorkflowRun.
	Actor string `json:"actor"`
	// OccurrenceKey deduplicates this run against its workflow definition.
	OccurrenceKey string `json:"occurrenceKey"`
	// CancelRequested is the user's stop intent. The operator fans it out to
	// the workflow's non-terminal step runs; the workflow only reaches
	// Canceled once its steps are observed terminal.
	CancelRequested bool `json:"cancelRequested,omitempty"`
}

// Validate checks the fields the CR shape can answer on its own. The operator
// adds the semantic gates: the revision must exist for the tenant, the
// revision's identity must match the workflow reference, and the occurrence
// key must not already belong to a terminal run of the same definition.
func (s WorkflowRunSpec) Validate() error {
	if s.WorkflowRef.Name == "" || s.WorkflowRef.UID == "" {
		return fmt.Errorf("workflowRunSpec.workflowRef requires name and uid")
	}
	if s.DefinitionRevision < 1 {
		return fmt.Errorf("workflowRunSpec.definitionRevision must be at least 1: revision ids start at 1")
	}
	if s.Trigger != Manual {
		return fmt.Errorf("workflowRunSpec.trigger must be %q: the WorkflowRun resource is the manual-trigger transport", Manual)
	}
	if s.Actor == "" {
		return fmt.Errorf("workflowRunSpec.actor is required")
	}
	if s.OccurrenceKey == "" {
		return fmt.Errorf("workflowRunSpec.occurrenceKey is required")
	}
	return nil
}
