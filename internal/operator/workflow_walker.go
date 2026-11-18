package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
	corepostgres "orbitjob/internal/core/store/postgres"
)

// ActorWorkflowWalker names the walker in the ledger when it creates a step.
// A step has no person behind it — the workflow's actor asked for the run, the
// walker only advances it — and job_run_control_plane.actor is NOT NULL with a
// non-empty CHECK, so the writer states who acted: the walker's role name.
const ActorWorkflowWalker = "orbitjob-workflow-walker"

var errWorkflowDurationUnavailable = errors.New("workflow duration unavailable")

// WorkflowPublisher publishes step runs as JobRun CRs and patches a stop
// request onto them. RunPublisher satisfies it; the narrow interface keeps the
// walker from reaching for the dynamic client itself.
type WorkflowPublisher interface {
	Publish(ctx context.Context, run v1alpha1.JobRun) error
	RequestCancel(ctx context.Context, namespace, name string, identity ...v1alpha1.RunIdentity) error
}

// WorkflowWalker advances open workflow runs: it creates ready tasks as
// ordinary runs of their referenced definitions, honors the fail policy and
// the workflow deadline, fans a stop request out to a stopping run's in-flight
// steps, and derives the workflow's phase from the ledger.
//
// The walker never touches the run pipeline a step travels after creation:
// its row is committed here, its CR is published here, and from then on the
// step is an ordinary run driven by ReconcileJobRun and ReconcileJob like any
// manual or scheduled run. That split is the design's spine — a workflow is
// not a second executor, it is a scheduler over the one run pipeline.
//
// One instance per Lease; the walker holds no state that matters, every tick
// re-derives intent from the ledger rows, so a crash or a leadership change
// anywhere in an advance leaves the next tick to converge on the same
// decisions.
type WorkflowWalker struct {
	Workflows WorkflowRunStore
	Revisions WorkflowRevisionSource
	Publisher WorkflowPublisher
	// ObserveResource verifies the bound live CR and reads its cancel intent.
	ObserveResource func(context.Context, string, workflow.Run) (bool, error)
	// Outcomes, when set, supplies a terminal step's attempt timing so
	// duration_ms conditions evaluate against the same fact the read models
	// see. Evaluating a duration rule without a reader is refused.
	Outcomes controlplane.OutcomeReader
	Tenants  []string
	// Actor is the identity recorded against every step this walker creates.
	// An empty value falls back to ActorWorkflowWalker, mirroring the
	// scheduler's actor resolution.
	Actor string
	// Now defaults to time.Now; tests pin it for deadline decisions.
	Now func() time.Time
	// ProjectStatus, when set, patches the run's CR status after the ledger
	// changed under it — the walker records phases between CR events, and
	// without this the read model would lag until the next resync. The
	// composition root wires it to the runtime's projection; unset means no
	// dynamic client is available and the read model heals on resync.
	ProjectStatus func(ctx context.Context, tenantID string, run workflow.Run) error
}

// Tick advances every open workflow run of every tenant and reports how many
// steps it created. A run that fails to advance is logged and skipped, so one
// wedged workflow cannot stop the others; only a failure to list a tenant's
// runs aborts the tick.
func (w WorkflowWalker) Tick(ctx context.Context) (int, error) {
	if w.Workflows == nil || w.Revisions == nil || w.Publisher == nil || w.ObserveResource == nil {
		return 0, fmt.Errorf("workflow walker dependencies are required")
	}
	created := 0
	for _, tenant := range w.Tenants {
		runs, err := w.Workflows.OpenRuns(ctx, tenant)
		if err != nil {
			return created, fmt.Errorf("list open workflow runs for tenant %s: %w", tenant, err)
		}
		if len(runs) == 0 {
			continue
		}
		defs, err := w.Revisions.ActiveRevisions(ctx, tenant)
		if err != nil {
			return created, fmt.Errorf("list active revisions for tenant %s: %w", tenant, err)
		}
		index := definitionIndex(defs)
		for _, run := range runs {
			n, err := w.advanceRun(ctx, tenant, run, index)
			if err != nil {
				slog.Error("advancing workflow run failed; skipping it",
					"tenant_id", tenant, "workflow_run_id", run.ID,
					"occurrence_key", run.OccurrenceKey, "error", err)
				continue
			}
			created += n
		}
	}
	return created, nil
}

// definitionIndex resolves task JobRefs and step source uids onto revisions.
// A task names its definition, a step row carries only the definition's uid;
// the active-revision list answers both directions in one read per tick.
func definitionIndex(defs []revision.Revision) map[string]revision.Revision {
	index := make(map[string]revision.Revision, len(defs))
	for _, rev := range defs {
		index["uid:"+rev.Identity.SourceUID] = rev
	}
	return index
}

// workflowNamespaceIndex prevents a same-name definition of another namespace or
// workload kind from entering a task's execution path.
func workflowNamespaceIndex(all map[string]revision.Revision, namespace string) map[string]revision.Revision {
	index := make(map[string]revision.Revision)
	for _, rev := range all {
		if rev.Identity.SourceMode == "kubernetes" && rev.Identity.Namespace == namespace {
			index["name:"+rev.Identity.Name] = rev
			index["uid:"+rev.Identity.SourceUID] = rev
		}
	}
	return index
}

func (w WorkflowWalker) actor() string {
	if w.Actor != "" {
		return w.Actor
	}
	return ActorWorkflowWalker
}

func (w WorkflowWalker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func (w WorkflowWalker) pastDeadline(run workflow.Run, dag workflow.DAG) bool {
	return dag.TimeoutSeconds > 0 && w.now().Sub(run.CreatedAt) > time.Duration(dag.TimeoutSeconds)*time.Second
}

// advanceRun moves one open workflow run to its next state and reports how
// many steps it created.
//
// The pass is one monotone sweep over the pinned DAG in topological order:
// steps that exist are only observed, tasks that are ready get their row and
// their CR, tasks that will never run get a decision with a reason, and the
// phase is derived from the ledger once every task is accounted for. Every
// write is idempotent or refused-and-harmless on replay, so a repeated pass
// lands on the same state.
func (w WorkflowWalker) advanceRun(ctx context.Context, tenant string, run workflow.Run, index map[string]revision.Revision) (int, error) {
	if run.ResourceUID == "" {
		workflowScheduleDecisions.WithLabelValues("waiting_publication").Inc()
		return 0, nil
	}
	cancel, err := w.ObserveResource(ctx, tenant, run)
	if err != nil {
		return 0, err
	}
	if cancel && run.Phase != workflow.PhaseCancelRequested && run.Phase != workflow.PhaseCancelUnknown && !workflow.Terminal(run.Phase) {
		if _, err := w.Workflows.UpdatePhase(ctx, tenant, run.ID, workflow.PhaseCancelRequested, nil); err != nil {
			return 0, err
		}
		run.Phase = workflow.PhaseCancelRequested
	}
	dag, namespace, err := w.pinnedDAG(ctx, tenant, run)
	if err != nil {
		return 0, err
	}

	index = workflowNamespaceIndex(index, namespace)
	if run.SnapshotVersion != 0 {
		index, err = w.snapshotDefinitions(ctx, tenant, run, dag, namespace)
		if err != nil {
			return 0, err
		}
	}
	steps, err := w.Workflows.Steps(ctx, tenant, run.ID)
	if err != nil {
		return 0, fmt.Errorf("list steps for workflow run %d: %w", run.ID, err)
	}
	canceling := run.Phase == workflow.PhaseCancelRequested || run.Phase == workflow.PhaseCancelUnknown
	deadlinePassed := false
	isStopping := func() bool {
		deadlinePassed = deadlinePassed || w.pastDeadline(run, dag)
		return canceling || deadlinePassed
	}
	stopRequested := false
	stopIfNeeded := func() error {
		if stopRequested || !isStopping() {
			return nil
		}
		stopRequested = true
		return w.fanCancel(ctx, tenant, namespace, run, dag, steps, index)
	}
	// Repair may create the CR needed for cancellation. A failure on one step
	// must still allow the stop request to reach every other step.
	repairErr := w.repairStepObjects(ctx, tenant, namespace, run, steps, index, isStopping)
	if err := errors.Join(repairErr, stopIfNeeded()); err != nil {
		return 0, err
	}
	stepByTask, states := stepFacts(run, dag, steps)

	// A stopping run creates no new steps. CancelUnknown retains unresolved
	// stop intent; retained trusted evidence may later confirm cancellation,
	// but uncertainty never permits more work. FailFast stops on the first
	// terminally failed step; in-flight
	// steps are respected, not preempted. The deadline is the workflow's own
	// wall clock: past it the run stops creating steps and asks the in-flight
	// ones to stop, the same mechanics a cancel fans out.
	failFastTripped := dag.FailPolicy == workflow.FailPolicyFailFast && anyStepFailed(steps)
	if dag.FailPolicy == workflow.FailPolicyFailFast {
		for _, decision := range run.TaskDecisions {
			if decision.Skipped && (decision.Reason == workflow.ReasonMissingJob || decision.Reason == workflow.ReasonDeadline) {
				failFastTripped = true
			}
		}
	}
	uncertain := run.Phase == workflow.PhaseExecutionUnknown
	for _, step := range steps {
		if step.Phase == jobrun.ExecutionUnknown || step.Phase == jobrun.CancelUnknown {
			uncertain = true
		}
	}

	order, err := workflow.TopologicalSort(dag)
	if err != nil {
		// ValidateDAG is the admission gate; a cycle here means the pinned
		// revision was written by something that skipped it. Refuse loudly
		// rather than advancing a partial order.
		return 0, fmt.Errorf("order workflow run %d: %w", run.ID, err)
	}

	decisions := make(map[string]workflow.TaskDecision)
	created := 0
	for _, taskName := range order {
		if _, hasStep := stepByTask[taskName]; hasStep {
			continue
		}
		if _, decided := run.TaskDecisions[taskName]; decided {
			continue
		}
		if uncertain && !stopRequested {
			continue
		}
		task, err := dagTask(dag, taskName)
		if err != nil {
			return created, err
		}
		// A task whose dependencies are neither terminal nor decided waits:
		// deciding it now would guess about work still in flight. Its
		// decision — run, skip, or cancel — is made the tick its
		// dependencies resolve.
		if !dependenciesResolved(task, states, run.TaskDecisions, decisions) {
			continue
		}

		reason := sweepStopReason(canceling, failFastTripped)
		if reason == "" {
			reason, err = w.taskSkipReason(ctx, tenant, task, stepByTask, states, run.TaskDecisions, decisions, run)
			// Reading a duration can carry the pass across the deadline.
			if stopErr := stopIfNeeded(); stopErr != nil {
				return created, errors.Join(err, stopErr)
			}
			if err != nil {
				if !deadlinePassed || !errors.Is(err, errWorkflowDurationUnavailable) {
					return created, err
				}
				slog.Warn("workflow deadline reached with unavailable condition duration",
					"tenant_id", tenant, "workflow_run_id", run.ID, "task", taskName, "error", err)
			}
		}
		if reason == "" && deadlinePassed {
			reason = workflow.ReasonDeadline
		}
		if reason != "" {
			decisions[taskName] = workflow.TaskDecision{Skipped: true, Reason: reason}
			continue
		}

		if _, found := index["name:"+task.JobRef]; !found {
			decisions[taskName] = workflow.TaskDecision{Skipped: true, Reason: workflow.ReasonMissingJob}
			workflowScheduleDecisions.WithLabelValues("missing_job_reference").Inc()
			slog.Warn("workflow task references an unavailable ScheduledJob", "tenant_id", tenant, "workflow_run_id", run.ID, "namespace", namespace, "task", task.Name, "job_ref", task.JobRef)
			if dag.FailPolicy == workflow.FailPolicyFailFast {
				failFastTripped = true
			}
			continue
		}
		step, createErr := w.createStep(ctx, tenant, namespace, run, task, index, isStopping)
		if step.ID != 0 {
			steps = append(steps, step)
		}
		switch {
		case createErr == nil:
			created++
		case errors.Is(createErr, jobrun.ErrAdmissionBlocked), errors.Is(createErr, corepostgres.ErrWorkflowBlocked):
			createErr = nil
		}
		if err := errors.Join(createErr, stopIfNeeded()); err != nil {
			return created, err
		}
	}

	if err := stopIfNeeded(); err != nil {
		return created, err
	}
	return created, w.recordOutcome(ctx, tenant, run, dag, steps, states, decisions, created, stopRequested)
}

// pinnedDAG loads the run's pinned workflow revision and decodes it into the
// walker's view. The revision — not the WorkflowJob's current spec — is what a
// run executes, so editing the definition cannot reshape a run already in
// flight.
func (w WorkflowWalker) pinnedDAG(ctx context.Context, tenant string, run workflow.Run) (workflow.DAG, string, error) {
	rev, err := w.Revisions.RevisionByID(ctx, tenant, run.RevisionID)
	if err != nil {
		return workflow.DAG{}, "", fmt.Errorf("load pinned revision %d for workflow run %d: %w", run.RevisionID, run.ID, err)
	}
	if rev.Identity.SourceMode != workflow.SourceModeWorkflow || rev.Identity.SourceUID != run.SourceUID {
		return workflow.DAG{}, "", fmt.Errorf("workflow run %d pinned identity mismatch", run.ID)
	}
	var declared v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return workflow.DAG{}, "", fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}
	dag := workflow.DAGFromSpec(declared)
	if err := workflow.ValidateDAG(dag); err != nil {
		return workflow.DAG{}, "", fmt.Errorf("workflow revision %d is not a valid DAG: %w", rev.ID, err)
	}
	return dag, rev.Identity.Namespace, nil
}

// stepFacts maps step rows back to their tasks — the ledger row does not carry
// the task name, so the walker re-derives each task's step occurrence key —
// and collects the operand set conditions and the phase derivation evaluate
// over. The set holds a task's state only once the step has resolved: a
// terminal phase, or CancelUnknown, which is terminal-but-undecided and
// dominates the roll-up. A step still running is absent, which is what keeps
// the workflow Running. Attempt timing is loaded only for conditions that the
// walker evaluates; cancellation and phase derivation need no timing operand.
func stepFacts(run workflow.Run, dag workflow.DAG, steps []workflow.StepRun) (map[string]workflow.StepRun, map[string]workflow.StepState) {
	byKey := make(map[string]workflow.StepRun, len(steps))
	for _, step := range steps {
		byKey[step.OccurrenceKey] = step
	}
	stepByTask := make(map[string]workflow.StepRun, len(steps))
	states := make(map[string]workflow.StepState, len(steps))
	for _, task := range dag.Tasks {
		step, ok := byKey[workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)]
		if !ok {
			continue
		}
		stepByTask[task.Name] = step
		if !jobrun.Terminal(step.Phase) && step.Phase != jobrun.CancelUnknown && step.Phase != jobrun.ExecutionUnknown {
			continue
		}
		states[task.Name] = workflow.StepState{Phase: step.Phase, Attempt: step.Attempt}
	}
	return stepByTask, states
}

// sweepStopReason selects the stop reason that takes precedence over task
// conditions. A deadline only supersedes work that cannot be skipped based
// on those conditions.
func sweepStopReason(canceling, failFastTripped bool) string {
	switch {
	case canceling:
		return workflow.ReasonCanceled
	case failFastTripped:
		return workflow.ReasonFailFast
	default:
		return ""
	}
}

// dependenciesResolved reports whether every dependency of the task has
// reached a fact the walker can act on: a resolved step (terminal or
// CancelUnknown) or a skip decision. An unresolved dependency means the task
// waits — work it depends on is still in flight.
func dependenciesResolved(
	task workflow.Task, states map[string]workflow.StepState,
	storedDecisions map[string]workflow.TaskDecision,
	passDecisions map[string]workflow.TaskDecision,
) bool {
	for _, dep := range task.DependsOn {
		if state, resolved := states[dep]; resolved && jobrun.Terminal(state.Phase) {
			continue
		}
		if decision, decided := storedDecisions[dep]; decided && decision.Skipped {
			continue
		}
		if _, decided := passDecisions[dep]; decided {
			continue
		}
		return false
	}
	return true
}

// taskSkipReason returns why a task whose dependencies have resolved will
// still never run. A skipped dependency never produced state to justify
// running the task, so the cascade is skipped with the condition reason — the
// honest closest of the four reasons for "an upstream this task depends on
// never ran". A declared condition is evaluated once, here, against the
// resolved states: EvaluateCondition errors when a rule reads a task with no
// recorded state, and since validation pins rules to dependencies and
// resolution pins dependencies to states, evaluation errors are surfaced.
// Missing duration evidence prevents evaluation independently of validation.
func (w WorkflowWalker) taskSkipReason(
	ctx context.Context, tenant string, task workflow.Task,
	steps map[string]workflow.StepRun, states map[string]workflow.StepState,
	storedDecisions map[string]workflow.TaskDecision,
	passDecisions map[string]workflow.TaskDecision, run workflow.Run,
) (string, error) {
	for _, dep := range task.DependsOn {
		if _, resolved := states[dep]; resolved {
			continue
		}
		if decision, decided := storedDecisions[dep]; decided && decision.Skipped {
			return workflow.ReasonCondition, nil
		}
		if _, decided := passDecisions[dep]; decided {
			return workflow.ReasonCondition, nil
		}
	}
	if task.Condition == nil {
		return "", nil
	}
	for _, rule := range task.Condition.Rules {
		if rule.Field != workflow.FieldDurationMs {
			continue
		}
		if w.Outcomes == nil {
			return "", fmt.Errorf("%w: workflow duration rules require outcome reader", errWorkflowDurationUnavailable)
		}
		step, found := steps[rule.Task]
		if !found || !jobrun.Terminal(step.Phase) {
			return "", fmt.Errorf("duration operand for task %s has no terminal step", rule.Task)
		}
		outcome, err := w.Outcomes.TerminalOutcome(ctx, tenant, step.ID)
		if err != nil {
			return "", fmt.Errorf("%w: read duration for terminal step %d: %w", errWorkflowDurationUnavailable, step.ID, err)
		}
		if outcome.StartedAt.IsZero() || outcome.CompletedAt.IsZero() {
			return "", fmt.Errorf("%w: terminal step %d has no measured duration", errWorkflowDurationUnavailable, step.ID)
		}
		state := states[rule.Task]
		state.DurationMs = outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds()
		states[rule.Task] = state
	}
	pass, err := workflow.EvaluateCondition(task.Condition, states)
	if err != nil {
		return "", fmt.Errorf("evaluate condition for task %s of workflow run %d: %w", task.Name, run.ID, err)
	}
	if !pass {
		return workflow.ReasonCondition, nil
	}
	return "", nil
}

// createStep commits the task's step row and publishes its JobRun CR, in that
// order: the row is the ledger's fact and the run reconciler refuses a CR
// without it, so a crash between the two leaves a row the next tick
// re-publishes for — never a CR the ledger does not know.
func (w WorkflowWalker) createStep(ctx context.Context, tenant, namespace string, run workflow.Run, task workflow.Task, index map[string]revision.Revision, isStopping func() bool) (workflow.StepRun, error) {
	def, ok := index["name:"+task.JobRef]
	if !ok || def.Identity.SourceMode != "kubernetes" || def.Identity.Namespace != namespace {
		return workflow.StepRun{}, fmt.Errorf("workflow run %d task %s references unknown definition %q", run.ID, task.Name, task.JobRef)
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(def.NormalizedSpec), &declared); err != nil {
		return workflow.StepRun{}, fmt.Errorf("decode definition %s revision %d: %w", def.Identity.Name, def.ID, err)
	}
	key := workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)
	stored, _, err := w.Workflows.CreateStepForTenant(ctx, tenant, run.ID, workflow.StepRun{
		SourceUID:     def.Identity.SourceUID,
		RevisionID:    def.ID,
		OccurrenceKey: key,
		Trigger:       jobrun.Workflow,
		Phase:         jobrun.Pending,
	}, declared.RetryPolicy.EffectiveMaxAttempts(), w.actor())
	if err != nil {
		return workflow.StepRun{}, fmt.Errorf("create step for task %s of workflow run %d: %w", task.Name, run.ID, err)
	}
	// Publish unconditionally, the scheduler's repair move: dedup makes the
	// row insert a no-op on replay, and an idempotent publish repairs a CR
	// lost to a crash between row and object.
	if err := w.repairStepObject(ctx, tenant, namespace, run, stored, index, isStopping); err != nil {
		return stored, fmt.Errorf("publish step for task %s of workflow run %d: %w", task.Name, run.ID, err)
	}
	return stored, nil
}

// stepObject renders the JobRun CR for one step: an ordinary run of the
// referenced definition, named by the shared run-name derivation so cancel
// and retention address it like any run. The task name travels nowhere on the
// CR: the step's membership in the workflow is the ledger's workflow_run_id
// pointer, and the walker maps back from rows to tasks by occurrence key.
func (w WorkflowWalker) stepObject(def revision.Revision, declared v1alpha1.ScheduledJobSpec, run workflow.Run, key string) v1alpha1.JobRun {
	return v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.GroupVersion.String(), Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v1alpha1.RunObjectName(def.Identity.Name, key),
			Namespace: def.Identity.Namespace,
			Labels: map[string]string{
				controlplane.LabelTenant:       def.Identity.Namespace,
				controlplane.LabelScheduledJob: def.Identity.SourceUID,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "ScheduledJob",
				Name:       def.Identity.Name,
				UID:        types.UID(def.Identity.SourceUID),
				// Deleting the definition garbage-collects its steps rather
				// than leaving orphans no reconciler owns.
				Controller:         ptr(true),
				BlockOwnerDeletion: ptr(false),
			}},
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef: v1alpha1.ObjectReference{
				Name: def.Identity.Name,
				UID:  def.Identity.SourceUID,
			},
			DefinitionRevision: def.ID,
			Trigger:            v1alpha1.Workflow,
			Actor:              w.actor(),
			OccurrenceKey:      key,
			// The deadline is pinned on the step, not read live from the
			// definition, so editing the definition cannot extend a step
			// already in flight.
			TimeoutSeconds: declared.TimeoutSeconds,
		},
	}
}

// recordOutcome merges this pass's skip decisions, advances the workflow's
// phase, and projects the run onto its CR when something changed.
//
// The phase write is one call even when decisions and a terminal verdict land
// together: UpdatePhase merges decisions per task and refuses a write to a run
// that already finished, so a replay of a finished run costs nothing.
// A child cancellation gap isolates a parent without its own stop intent.
func (w WorkflowWalker) recordOutcome(
	ctx context.Context, tenant string, run workflow.Run, dag workflow.DAG,
	steps []workflow.StepRun, states map[string]workflow.StepState,
	decisions map[string]workflow.TaskDecision, created int, stopping bool,
) error {
	merged := mergeDecisions(run, decisions)
	phase, terminal := workflow.DerivePhase(taskNames(dag), states, merged)
	next := run.Phase
	switch {
	case terminal && (run.Phase == workflow.PhaseCancelRequested || run.Phase == workflow.PhaseCancelUnknown):
		next = workflow.PhaseCanceled
	case terminal:
		next = phase
	case phase == workflow.PhaseCancelUnknown:
		if stopping {
			next = phase
		} else {
			// A stop of one child is not a stop request for its siblings.
			next = workflow.PhaseExecutionUnknown
		}
	case phase == workflow.PhaseExecutionUnknown && !stopping:
		next = phase
	case run.Phase == workflow.PhaseExecutionUnknown && phase == workflow.PhaseRunning:
		next = workflow.PhaseRunning
	case run.Phase == workflow.PhasePending && (len(steps) > 0 || len(decisions) > 0 || created > 0):
		next = workflow.PhaseRunning
	}
	if next == run.Phase && len(decisions) == 0 {
		// Nothing about the run moved this pass; a repeated phase write would
		// only cost a row update and a projection.
		return nil
	}

	changed, err := w.Workflows.UpdatePhase(ctx, tenant, run.ID, next, decisions)
	if err != nil {
		return fmt.Errorf("record workflow run %d phase: %w", run.ID, err)
	}
	if !changed && len(decisions) == 0 {
		// The store refused or repeated the write and no decision landed:
		// nothing about the run changed, so there is nothing to project.
		return nil
	}
	run.Phase = next
	run.TaskDecisions = merged
	if w.ProjectStatus == nil {
		return nil
	}
	return w.ProjectStatus(ctx, tenant, run)
}

// fanCancel patches the stop request onto every in-flight step of a stopping
// run. Terminal steps are done and left alone; a CancelUnknown step is
// deliberately skipped because its cancellation intent is already committed.
// Its recovery path can still confirm a trusted stop after a deletion delay.
// Each patched step uses the ordinary per-run cancellation path: controller
// acknowledgement, persisted termination evidence, hold release and confirmed
// Job removal. The workflow-level Canceled waits for those facts.
func (w WorkflowWalker) fanCancel(ctx context.Context, tenant, namespace string, run workflow.Run, dag workflow.DAG, steps []workflow.StepRun, index map[string]revision.Revision) error {
	byKey := make(map[string]workflow.StepRun, len(steps))
	for _, step := range steps {
		byKey[step.OccurrenceKey] = step
	}
	var failures []error
	for _, task := range dag.Tasks {
		step, ok := byKey[workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)]
		if !ok || jobrun.Terminal(step.Phase) || step.Phase == jobrun.CancelUnknown {
			continue
		}
		def, err := w.stepRevision(ctx, tenant, namespace, run, step, index)
		if err == nil {
			identity := v1alpha1.RunIdentity{SourceUID: step.SourceUID, OccurrenceKey: step.OccurrenceKey, RevisionID: step.RevisionID, Trigger: v1alpha1.Workflow}
			err = w.Publisher.RequestCancel(ctx, namespace, def.Identity.Name, identity)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("request cancel for step %s of workflow run %d: %w", task.Name, run.ID, err))
		}
	}
	return errors.Join(failures...)
}

func ptr[T any](v T) *T { return &v }

func anyStepFailed(steps []workflow.StepRun) bool {
	for _, step := range steps {
		if step.Phase == jobrun.Failed {
			return true
		}
	}
	return false
}

func taskNames(dag workflow.DAG) []string {
	names := make([]string, 0, len(dag.Tasks))
	for _, task := range dag.Tasks {
		names = append(names, task.Name)
	}
	return names
}

func dagTask(dag workflow.DAG, name string) (workflow.Task, error) {
	for _, task := range dag.Tasks {
		if task.Name == name {
			return task, nil
		}
	}
	return workflow.Task{}, fmt.Errorf("task %s not found in workflow DAG", name)
}

// mergeDecisions folds the pass's decisions over the row's stored trail. The
// store merges per task name too; folding here keeps the in-memory view the
// derivation and the projection see identical to what the write stores.
func mergeDecisions(run workflow.Run, decisions map[string]workflow.TaskDecision) map[string]workflow.TaskDecision {
	if len(decisions) == 0 {
		return run.TaskDecisions
	}
	merged := make(map[string]workflow.TaskDecision, len(run.TaskDecisions)+len(decisions))
	for name, decision := range run.TaskDecisions {
		merged[name] = decision
	}
	for name, decision := range decisions {
		merged[name] = decision
	}
	return merged
}
