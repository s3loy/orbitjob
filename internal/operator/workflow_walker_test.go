package operator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// --- helpers ----------------------------------------------------------------

func walkerRun() workflow.Run {
	return workflow.Run{
		ID: 42, TenantID: "tenant-a", SourceUID: "wf-uid-1", RevisionID: 5,
		OccurrenceKey: "wocc-1", Trigger: "Manual", Actor: "principal-key-1",
		Phase:     workflow.PhasePending,
		CreatedAt: time.Unix(1699999000, 0).UTC(), UpdatedAt: time.Unix(1699999000, 0).UTC(),
	}
}

// jobSpecJSON is the ScheduledJob spec the referenced definitions carry: a
// real image and a two-retry budget, so the walker's step rendering decodes
// honest values.
func jobSpecJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(v1alpha1.ScheduledJobSpec{
		Schedule:    "* * * * *",
		JobTemplate: v1alpha1.JobTemplateSpec{Image: "registry.example.com/step:v1"},
		RetryPolicy: v1alpha1.RetryPolicy{MaxAttempts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// walkerRevisions pins revisions 5 and 6 to the shared two-task spec
// (extract → load) and registers both referenced definitions as active.
func walkerRevisions(t *testing.T) *fakeWorkflowRevisions {
	t.Helper()
	spec := workflowSpecJSON(t, twoTaskSpec())
	rev := workflowRevision(t, 5, spec)
	rev6 := workflowRevision(t, 6, spec)
	extract := withIdentity(t, workflowRevision(t, 10, jobSpecJSON(t)), "kubernetes", "extract-job-uid", "finance", "extract-job")
	load := withIdentity(t, workflowRevision(t, 11, jobSpecJSON(t)), "kubernetes", "load-job-uid", "finance", "load-job")
	return &fakeWorkflowRevisions{
		byID:   map[int64]revision.Revision{5: rev, 6: rev6, 10: extract, 11: load},
		active: []revision.Revision{extract, load},
	}
}

func withIdentity(t *testing.T, rev revision.Revision, mode, uid, ns, name string) revision.Revision {
	t.Helper()
	next, err := revision.New(revision.Identity{
		SourceMode: mode, SourceUID: uid, Namespace: ns, Name: name,
	}, 1, rev.NormalizedSpec, "operator", "", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	next.ID = rev.ID
	return next
}

func newWalker(t *testing.T, runs *fakeWorkflowRuns, revs *fakeWorkflowRevisions, pub *fakeWorkflowPublisher) (*WorkflowWalker, *[]workflow.Run) {
	t.Helper()
	projected := &[]workflow.Run{}
	walker := &WorkflowWalker{
		Workflows: runs,
		Revisions: revs,
		Publisher: pub,
		Tenants:   []string{"tenant-a"},
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
		ProjectStatus: func(_ context.Context, _ string, run workflow.Run) error {
			*projected = append(*projected, run)
			return nil
		},
	}
	return walker, projected
}

func stepKey(t *testing.T, task string) string {
	t.Helper()
	return workflow.StepOccurrenceKey("wf-uid-1", "wocc-1", task)
}

func stepFor(t *testing.T, task string, phase jobrun.Phase, attempt int) workflow.StepRun {
	t.Helper()
	return workflow.StepRun{
		ID: 1, WorkflowRunID: 42, SourceUID: task + "-job-uid", OccurrenceKey: stepKey(t, task),
		Trigger: jobrun.Workflow, Phase: phase, Attempt: attempt,
	}
}

// --- advancement -------------------------------------------------------------

// TestWorkflowWalkerCreatesRootSteps pins the basic advance: a Pending run
// with no steps gets its root tasks' rows and CRs, the CR carries the
// Workflow trigger and the derived step key, and the run leaves Pending.
func TestWorkflowWalkerCreatesRootSteps(t *testing.T) {
	runs := &fakeWorkflowRuns{stored: walkerRun()}
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	created, err := walker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("created = %d, want only the root task (extract); load depends on it", created)
	}
	if len(runs.createdStep) != 1 || runs.createdStep[0].OccurrenceKey != stepKey(t, "extract") {
		t.Fatalf("created steps = %+v, want exactly extract's step", runs.createdStep)
	}
	if runs.createdStep[0].Trigger != jobrun.Workflow || runs.createdStep[0].SourceUID != "extract-job-uid" {
		t.Fatalf("step = %+v, want a Workflow run of the referenced definition", runs.createdStep[0])
	}
	if len(pub.published) != 1 {
		t.Fatalf("published = %d, want 1", len(pub.published))
	}
	published := pub.published[0]
	wantName := v1alpha1.RunObjectName("extract-job", stepKey(t, "extract"))
	if published.Name != wantName || published.Namespace != "finance" {
		t.Fatalf("published name/ns = %s/%s, want %s", published.Name, published.Namespace, wantName)
	}
	if published.Spec.Trigger != v1alpha1.Workflow || published.Spec.OccurrenceKey != stepKey(t, "extract") ||
		published.Spec.DefinitionRevision != 10 || published.Spec.Actor != ActorWorkflowWalker {
		t.Fatalf("published spec = %+v, want the pinned Workflow step", published.Spec)
	}
	if runs.phases[len(runs.phases)-1].phase != workflow.PhaseRunning {
		t.Fatalf("last phase write = %+v, want the run advanced to Running", runs.phases)
	}
}

// TestWorkflowWalkerWaitsForNonTerminalDependencies pins ordering: a task
// whose dependency has not reached a terminal step is neither created nor
// decided, no matter how many ticks pass.
func TestWorkflowWalkerWaitsForNonTerminalDependencies(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Running, 1)}}
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	created, err := walker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(runs.createdStep) != 0 {
		t.Fatalf("created = %d (%+v), want 0: load's dependency is still in flight", created, runs.createdStep)
	}
	if len(runs.phases) != 0 || len(pub.published) != 0 {
		t.Fatalf("unexpected writes: phases=%+v published=%+v", runs.phases, pub.published)
	}
}

// TestWorkflowWalkerRunsDependentAfterTerminal pins the release: when the
// dependency's step is terminal, the dependent task's row and CR are created.
func TestWorkflowWalkerRunsDependentAfterTerminal(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	created, err := walker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(runs.createdStep) != 1 || runs.createdStep[0].OccurrenceKey != stepKey(t, "load") {
		t.Fatalf("created = %d steps %+v, want exactly load's step", created, runs.createdStep)
	}
}

// TestWorkflowWalkerConditionSkipRecordsDecision pins condition evaluation:
// a failed condition skips the task without creating any run, and the run
// converges to Succeeded over the terminal step plus the decision.
func TestWorkflowWalkerConditionSkipRecordsDecision(t *testing.T) {
	spec := twoTaskSpec()
	// load runs only when extract failed; extract succeeded, so load is skipped.
	cond := &v1alpha1.WorkflowConditionSpec{
		Type:  v1alpha1.ConditionAll,
		Rules: []v1alpha1.WorkflowRule{{Task: "extract", Field: v1alpha1.RuleFieldPhase, Operator: v1alpha1.OperatorEq, Values: []string{"Failed"}}},
	}
	spec.Tasks[1].Condition = cond
	wfRev := workflowRevision(t, 5, workflowSpecJSON(t, spec))
	revs := &fakeWorkflowRevisions{
		byID: map[int64]revision.Revision{5: wfRev},
		active: []revision.Revision{
			withIdentity(t, workflowRevision(t, 10, jobSpecJSON(t)), "kubernetes", "extract-job-uid", "finance", "extract-job"),
			withIdentity(t, workflowRevision(t, 11, jobSpecJSON(t)), "kubernetes", "load-job-uid", "finance", "load-job"),
		},
	}
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Succeeded, 1)}}
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runs.createdStep) != 0 {
		t.Fatalf("created steps = %+v, want 0: the condition failed", runs.createdStep)
	}
	last := runs.phases[len(runs.phases)-1]
	if last.phase != workflow.PhaseSucceeded {
		t.Fatalf("phase = %s, want Succeeded over the step and the skip decision", last.phase)
	}
	if len(last.keys) != 1 || last.keys[0] != "load" {
		t.Fatalf("decision keys = %+v, want [load]", last.keys)
	}
}

// TestWorkflowWalkerFailPolicy pins the fail policy: FailFast stops new work
// with the fail_fast reason once a step failed terminally; Continue lets the
// ready task proceed.
func TestWorkflowWalkerFailPolicy(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	steps := []workflow.StepRun{stepFor(t, "extract", jobrun.Failed, 2)}

	t.Run("FailFast skips with reason fail_fast", func(t *testing.T) {
		revs := walkerRevisions(t)
		runs := &fakeWorkflowRuns{stored: run, steps: steps}
		pub := &fakeWorkflowPublisher{}
		walker, _ := newWalker(t, runs, revs, pub)

		if _, err := walker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(runs.createdStep) != 0 {
			t.Fatalf("created steps = %+v, want 0 under FailFast", runs.createdStep)
		}
		last := runs.phases[len(runs.phases)-1]
		if last.phase != workflow.PhaseFailed || len(last.keys) != 1 || last.keys[0] != "load" {
			t.Fatalf("phase write = %+v, want Failed with a fail_fast decision for load", last)
		}
	})
	t.Run("Continue creates the ready task", func(t *testing.T) {
		spec := twoTaskSpec()
		spec.FailPolicy = v1alpha1.Continue
		revs := &fakeWorkflowRevisions{
			byID:   map[int64]revision.Revision{5: workflowRevision(t, 5, workflowSpecJSON(t, spec))},
			active: walkerRevisions(t).active,
		}
		runs := &fakeWorkflowRuns{stored: run, steps: steps}
		pub := &fakeWorkflowPublisher{}
		walker, _ := newWalker(t, runs, revs, pub)

		if _, err := walker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(runs.createdStep) != 1 || runs.createdStep[0].OccurrenceKey != stepKey(t, "load") {
			t.Fatalf("created steps = %+v, want load to proceed under Continue", runs.createdStep)
		}
	})
}

// TestWorkflowWalkerCancelFansOutToInFlightSteps pins the cancel fan-out: a
// run at CancelRequested stops creating steps, patches the stop request onto
// its in-flight steps only, and records canceled decisions for tasks that
// never started. The workflow converges to Canceled once every step is
// terminal.
func TestWorkflowWalkerCancelFansOutToInFlightSteps(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseCancelRequested
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{
		stepFor(t, "extract", jobrun.Running, 1),
		stepFor(t, "load", jobrun.Succeeded, 1),
	}}
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The DAG is extract → load; extract is in flight, load is terminal.
	want := v1alpha1.RunObjectName("extract-job", stepKey(t, "extract"))
	if len(pub.cancels) != 1 || pub.cancels[0] != want {
		t.Fatalf("cancels = %+v, want exactly [%s]", pub.cancels, want)
	}
	if len(runs.phases) != 0 {
		t.Fatalf("phase writes = %+v, want none: extract is still in flight and the row already carries the request", runs.phases)
	}

	// Next tick, extract observed Canceled: every task accounted, run done.
	runs.steps = []workflow.StepRun{
		stepFor(t, "extract", jobrun.Canceled, 1),
		stepFor(t, "load", jobrun.Succeeded, 1),
	}
	runs.stored.TaskDecisions = map[string]workflow.TaskDecision{}
	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := runs.phases[len(runs.phases)-1]; got.phase != workflow.PhaseCanceled {
		t.Fatalf("phase = %s, want Canceled once every step is terminal", got.phase)
	}
}

// TestWorkflowWalkerCancelSkipsCancelUnknownStep pins the dominance rule: a
// step in CancelUnknown is never re-patched (its lifecycle refuses the
// transition), and the workflow stays CancelUnknown and non-terminal — the
// same state its step is in.
func TestWorkflowWalkerCancelSkipsCancelUnknownStep(t *testing.T) {
	run := walkerRun()
	run.Phase = workflow.PhaseCancelRequested
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{
		stepFor(t, "extract", jobrun.CancelUnknown, 1),
	}}
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.cancels) != 0 {
		t.Fatalf("cancels = %+v, want none: a CancelUnknown step must not be re-patched", pub.cancels)
	}
	got := runs.phases[len(runs.phases)-1]
	if got.phase != workflow.PhaseCancelUnknown {
		t.Fatalf("phase = %s, want CancelUnknown: the unknown dominates and stays non-terminal", got.phase)
	}
}

// TestWorkflowWalkerDeadlineStopsAndFansOut pins the workflow deadline: past
// it, no new steps are created, uncreated tasks get the deadline decision,
// and in-flight steps are asked to stop.
func TestWorkflowWalkerDeadlineStopsAndFansOut(t *testing.T) {
	spec := twoTaskSpec()
	spec.TimeoutSeconds = 60
	revs := &fakeWorkflowRevisions{
		byID:   map[int64]revision.Revision{5: workflowRevision(t, 5, workflowSpecJSON(t, spec))},
		active: walkerRevisions(t).active,
	}
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{stepFor(t, "extract", jobrun.Running, 1)}}
	pub := &fakeWorkflowPublisher{}
	walker, _ := newWalker(t, runs, revs, pub)

	if _, err := walker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runs.createdStep) != 0 {
		t.Fatalf("created steps = %+v, want 0 past the deadline", runs.createdStep)
	}
	want := v1alpha1.RunObjectName("extract-job", stepKey(t, "extract"))
	if len(pub.cancels) != 1 || pub.cancels[0] != want {
		t.Fatalf("cancels = %+v, want the in-flight step asked to stop", pub.cancels)
	}
	if len(runs.phases) != 0 {
		t.Fatalf("phase writes = %+v, want none: the in-flight step has not confirmed the stop yet", runs.phases)
	}
}

// TestWorkflowWalkerIsIdempotentOnReplay pins convergence: the first pass
// over a run whose steps all finished records the terminal verdict; the
// second pass creates nothing, writes nothing, and projects nothing, because
// the store refuses terminal bookkeeping twice.
func TestWorkflowWalkerIsIdempotentOnReplay(t *testing.T) {
	revs := walkerRevisions(t)
	pub := &fakeWorkflowPublisher{}
	run := walkerRun()
	run.Phase = workflow.PhaseRunning
	run.TaskDecisions = map[string]workflow.TaskDecision{}
	runs := &fakeWorkflowRuns{stored: run, steps: []workflow.StepRun{
		stepFor(t, "extract", jobrun.Succeeded, 1),
		stepFor(t, "load", jobrun.Succeeded, 1),
	}}
	walker, projected := newWalker(t, runs, revs, pub)

	created, err := walker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(runs.phases) != 1 || runs.phases[0].phase != workflow.PhaseSucceeded {
		t.Fatalf("first pass: created=%d phases=%+v, want one Succeeded verdict", created, runs.phases)
	}

	// Replay: the row is terminal now; nothing may happen again.
	created, err = walker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || len(runs.phases) != 1 || len(*projected) != 1 {
		t.Fatalf("replay: created=%d phases=%d projected=%d, want all unchanged: terminal bookkeeping fires once",
			created, len(runs.phases), len(*projected))
	}
}

// TestWorkflowWalkerSurvivesOneWedgedRun pins tick isolation: a run whose
// pinned revision is missing is logged and skipped, and the next run still
// advances.
func TestWorkflowWalkerSurvivesOneWedgedRun(t *testing.T) {
	pub := &fakeWorkflowPublisher{}
	wedged := walkerRun()
	good := walkerRun()
	good.ID = 43
	good.OccurrenceKey = "wocc-2"
	good.RevisionID = 6
	revs := walkerRevisions(t)
	delete(revs.byID, 5)
	runs := &fakeWorkflowRuns{stored: good, openRuns: []workflow.Run{wedged, good}}
	walker, _ := newWalker(t, runs, revs, pub)

	created, err := walker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || len(runs.createdStep) != 1 {
		t.Fatalf("created = %d, want the healthy run's root step only", created)
	}
}
