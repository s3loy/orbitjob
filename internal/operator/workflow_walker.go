package operator

import (
	"context"
	"encoding/json"
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
)

// ActorWorkflowWalker names the walker in the ledger when it creates a step.
// A step has no person behind it — the workflow's actor asked for the run, the
// walker only advances it — and job_run_control_plane.actor is NOT NULL with a
// non-empty CHECK, so the writer states who acted: the walker's role name.
const ActorWorkflowWalker = "orbitjob-workflow-walker"

// WorkflowPublisher publishes step runs as JobRun CRs and patches a stop
// request onto them. RunPublisher satisfies it; the narrow interface keeps the
// walker from reaching for the dynamic client itself.
type WorkflowPublisher interface {
	Publish(ctx context.Context, run v1alpha1.JobRun) error
	RequestCancel(ctx context.Context, namespace, name string) error
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
	// Outcomes, when set, supplies a terminal step's attempt timing so
	// duration_ms conditions evaluate against the same fact the read models
	// see. Without it those rules read a zero duration.
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
	if w.Workflows == nil || w.Revisions == nil || w.Publisher == nil {
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
	index := make(map[string]revision.Revision, 2*len(defs))
	for _, rev := range defs {
		index["name:"+rev.Identity.Name] = rev
		index["uid:"+rev.Identity.SourceUID] = rev
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
	dag, err := w.pinnedDAG(ctx, tenant, run)
	if err != nil {
		return 0, err
	}

	steps, err := w.Workflows.Steps(ctx, tenant, run.ID)
	if err != nil {
		return 0, fmt.Errorf("list steps for workflow run %d: %w", run.ID, err)
	}
	stepByTask, states := w.stepFacts(ctx, tenant, run, dag, steps)

	// A stopping run creates no new steps. CancelRequested is a user's stop;
	// CancelUnknown is a stop the platform could not confirm, whose workflow
	// can never complete — a CancelUnknown step has no transition out — so
	// creating more work under either would be pretending the stop did not
	// happen. FailFast stops on the first terminally failed step; in-flight
	// steps are respected, not preempted. The deadline is the workflow's own
	// wall clock: past it the run stops creating steps and asks the in-flight
	// ones to stop, the same mechanics a cancel fans out.
	canceling := run.Phase == workflow.PhaseCancelRequested || run.Phase == workflow.PhaseCancelUnknown
	failFastTripped := dag.FailPolicy == workflow.FailPolicyFailFast && anyStepFailed(steps)
	deadlinePassed := dag.TimeoutSeconds > 0 && w.now().Sub(run.CreatedAt) > time.Duration(dag.TimeoutSeconds)*time.Second

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

		reason := sweepStopReason(canceling, failFastTripped, deadlinePassed)
		if reason == "" {
			reason, err = taskSkipReason(task, states, run.TaskDecisions, decisions, run)
			if err != nil {
				return created, err
			}
		}
		if reason != "" {
			decisions[taskName] = workflow.TaskDecision{Skipped: true, Reason: reason}
			continue
		}

		if err := w.createStep(ctx, tenant, run, task, index); err != nil {
			return created, err
		}
		created++
	}

	return created, w.recordOutcome(ctx, tenant, run, dag, steps, states, decisions, created, canceling || deadlinePassed, index)
}

// pinnedDAG loads the run's pinned workflow revision and decodes it into the
// walker's view. The revision — not the WorkflowJob's current spec — is what a
// run executes, so editing the definition cannot reshape a run already in
// flight.
func (w WorkflowWalker) pinnedDAG(ctx context.Context, tenant string, run workflow.Run) (workflow.DAG, error) {
	rev, err := w.Revisions.RevisionByID(ctx, tenant, run.RevisionID)
	if err != nil {
		return workflow.DAG{}, fmt.Errorf("load pinned revision %d for workflow run %d: %w", run.RevisionID, run.ID, err)
	}
	var declared v1alpha1.WorkflowJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &declared); err != nil {
		return workflow.DAG{}, fmt.Errorf("decode pinned revision %d: %w", rev.ID, err)
	}
	dag := workflow.DAGFromSpec(declared)
	if err := workflow.ValidateDAG(dag); err != nil {
		return workflow.DAG{}, fmt.Errorf("workflow revision %d is not a valid DAG: %w", rev.ID, err)
	}
	return dag, nil
}

// stepFacts maps step rows back to their tasks — the ledger row does not carry
// the task name, so the walker re-derives each task's step occurrence key —
// and collects the operand set conditions and the phase derivation evaluate
// over. The set holds a task's state only once the step has resolved: a
// terminal phase, or CancelUnknown, which is terminal-but-undecided and
// dominates the roll-up. A step still running is absent, which is what keeps
// the workflow Running. A resolved step's attempt timing is not on its row, so
// duration_ms operands are enriched from the same terminal-outcome read the
// read models use, only when the walker was wired with a reader.
func (w WorkflowWalker) stepFacts(ctx context.Context, tenant string, run workflow.Run, dag workflow.DAG, steps []workflow.StepRun) (map[string]workflow.StepRun, map[string]workflow.StepState) {
	byKey := make(map[string]workflow.StepRun, len(steps))
	for _, step := range steps {
		byKey[step.OccurrenceKey] = step
	}
	needDurations := dagHasDurationRules(dag) && w.Outcomes != nil

	stepByTask := make(map[string]workflow.StepRun, len(steps))
	states := make(map[string]workflow.StepState, len(steps))
	for _, task := range dag.Tasks {
		step, ok := byKey[workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)]
		if !ok {
			continue
		}
		stepByTask[task.Name] = step
		if !jobrun.Terminal(step.Phase) && step.Phase != jobrun.CancelUnknown {
			continue
		}
		state := workflow.StepState{Phase: step.Phase, Attempt: step.Attempt}
		if needDurations && jobrun.Terminal(step.Phase) {
			outcome, err := w.Outcomes.TerminalOutcome(ctx, tenant, step.ID)
			if err == nil && !outcome.StartedAt.IsZero() && !outcome.CompletedAt.IsZero() {
				state.DurationMs = outcome.CompletedAt.Sub(outcome.StartedAt).Milliseconds()
			}
		}
		states[task.Name] = state
	}
	return stepByTask, states
}

// sweepStopReason decides whether a sweep-level stop applies to a task that
// has neither a step nor a decision yet. Precedence follows what actually
// happened to the run: a stop first, then the fail policy, then the deadline
// the run outlived.
func sweepStopReason(canceling, failFastTripped, deadlinePassed bool) string {
	switch {
	case canceling:
		return workflow.ReasonCanceled
	case failFastTripped:
		return workflow.ReasonFailFast
	case deadlinePassed:
		return workflow.ReasonDeadline
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
		if _, resolved := states[dep]; resolved {
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
// resolution pins dependencies to states, an error is a walker bug and is
// surfaced, never swallowed into a skip.
func taskSkipReason(
	task workflow.Task, states map[string]workflow.StepState,
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
func (w WorkflowWalker) createStep(ctx context.Context, tenant string, run workflow.Run, task workflow.Task, index map[string]revision.Revision) error {
	def, ok := index["name:"+task.JobRef]
	if !ok {
		return fmt.Errorf("workflow run %d task %s references unknown definition %q", run.ID, task.Name, task.JobRef)
	}
	var declared v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(def.NormalizedSpec), &declared); err != nil {
		return fmt.Errorf("decode definition %s revision %d: %w", def.Identity.Name, def.ID, err)
	}
	key := workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)
	if _, _, err := w.Workflows.CreateStepForTenant(ctx, tenant, run.ID, workflow.StepRun{
		SourceUID:     def.Identity.SourceUID,
		RevisionID:    def.ID,
		OccurrenceKey: key,
		Trigger:       jobrun.Workflow,
		Phase:         jobrun.Pending,
	}, declared.RetryPolicy.EffectiveMaxAttempts(), w.actor()); err != nil {
		return fmt.Errorf("create step for task %s of workflow run %d: %w", task.Name, run.ID, err)
	}
	// Publish unconditionally, the scheduler's repair move: dedup makes the
	// row insert a no-op on replay, and an idempotent publish repairs a CR
	// lost to a crash between row and object.
	if err := w.Publisher.Publish(ctx, w.stepObject(def, declared, run, key)); err != nil {
		return fmt.Errorf("publish step for task %s of workflow run %d: %w", task.Name, run.ID, err)
	}
	return nil
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
// that already finished, so a replay of a finished run costs nothing. A
// workflow with a CancelUnknown step records CancelUnknown once — it is sticky
// and non-terminal, and the store refuses every later write, which is exactly
// the once-only bookkeeping the phase needs.
func (w WorkflowWalker) recordOutcome(
	ctx context.Context, tenant string, run workflow.Run, dag workflow.DAG,
	steps []workflow.StepRun, states map[string]workflow.StepState,
	decisions map[string]workflow.TaskDecision, created int, fanCancel bool,
	index map[string]revision.Revision,
) error {
	if fanCancel {
		if err := w.fanCancel(ctx, tenant, run, dag, steps, index); err != nil {
			return err
		}
	}

	merged := mergeDecisions(run, decisions)
	phase, terminal := workflow.DerivePhase(taskNames(dag), states, merged)
	next := run.Phase
	switch {
	case terminal:
		next = phase
	case phase == workflow.PhaseCancelUnknown:
		// CancelUnknown dominates the roll-up and the row alike: a stop the
		// platform could not confirm is not an outcome, the workflow stays
		// non-terminal, and no later phase — not even a recorded stop
		// request — may overwrite the one honest signal the row holds. Every
		// decision-trail write that follows carries the same phase until the
		// unknown resolves.
		next = phase
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
// deliberately skipped — its run already carries a stop the platform could
// not confirm, and re-patching would push its reconciliation into a phase
// transition the run lifecycle refuses. Each patched step then travels the
// ordinary per-run cancel path, which deletes the Kubernetes Job and observes
// it gone before recording Canceled; the workflow-level Canceled waits for
// those facts, it does not presume them.
func (w WorkflowWalker) fanCancel(ctx context.Context, tenant string, run workflow.Run, dag workflow.DAG, steps []workflow.StepRun, index map[string]revision.Revision) error {
	byKey := make(map[string]workflow.StepRun, len(steps))
	for _, step := range steps {
		byKey[step.OccurrenceKey] = step
	}
	for _, task := range dag.Tasks {
		step, ok := byKey[workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)]
		if !ok || jobrun.Terminal(step.Phase) || step.Phase == jobrun.CancelUnknown {
			continue
		}
		def, ok := index["name:"+task.JobRef]
		if !ok {
			// The definition is gone; its CRs went with it via the owner
			// reference, so there is nothing to patch and nothing will run.
			continue
		}
		key := workflow.StepOccurrenceKey(run.SourceUID, run.OccurrenceKey, task.Name)
		name := v1alpha1.RunObjectName(def.Identity.Name, key)
		if err := w.Publisher.RequestCancel(ctx, def.Identity.Namespace, name); err != nil {
			return fmt.Errorf("request cancel for step %s of workflow run %d: %w", task.Name, run.ID, err)
		}
	}
	return nil
}

func dagHasDurationRules(dag workflow.DAG) bool {
	for _, task := range dag.Tasks {
		if task.Condition == nil {
			continue
		}
		for _, rule := range task.Condition.Rules {
			if rule.Field == workflow.FieldDurationMs {
				return true
			}
		}
	}
	return false
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
