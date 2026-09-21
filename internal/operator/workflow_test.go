package operator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/revision"
	"orbitjob/internal/core/domain/workflow"
)

// --- fakes ------------------------------------------------------------------

type fakeWorkflowRuns struct {
	stored      workflow.Run
	found       bool
	lookupErr   error
	openRuns    []workflow.Run
	created     []workflow.Run
	createErr   error
	steps       []workflow.StepRun
	stepsErr    error
	createdStep []workflow.StepRun
	stepErr     error
	phases      []workflowPhaseWrite
	phaseErr    error
}

type workflowPhaseWrite struct {
	runID int64
	phase workflow.Phase
	keys  []string
}

func (f *fakeWorkflowRuns) CreateRunForTenant(_ context.Context, _ string, run workflow.Run) (workflow.Run, bool, error) {
	if f.createErr != nil {
		return workflow.Run{}, false, f.createErr
	}
	f.created = append(f.created, run)
	f.stored = run
	f.found = true
	return run, true, nil
}

func (f *fakeWorkflowRuns) RunByOccurrence(_ context.Context, _, _, _ string) (workflow.Run, bool, error) {
	return f.stored, f.found, f.lookupErr
}

func (f *fakeWorkflowRuns) OpenRuns(_ context.Context, _ string) ([]workflow.Run, error) {
	if f.openRuns != nil {
		return f.openRuns, nil
	}
	return []workflow.Run{f.stored}, nil
}

func (f *fakeWorkflowRuns) Steps(_ context.Context, _ string, _ int64) ([]workflow.StepRun, error) {
	if f.stepsErr != nil {
		return nil, f.stepsErr
	}
	return f.steps, nil
}

func (f *fakeWorkflowRuns) CreateStepForTenant(_ context.Context, _ string, workflowRunID int64, step workflow.StepRun, _ int, _ string) (workflow.StepRun, bool, error) {
	if f.stepErr != nil {
		return workflow.StepRun{}, false, f.stepErr
	}
	step.ID = int64(len(f.createdStep) + 1)
	step.WorkflowRunID = workflowRunID
	f.createdStep = append(f.createdStep, step)
	return step, true, nil
}

func (f *fakeWorkflowRuns) UpdatePhase(_ context.Context, _ string, runID int64, phase workflow.Phase, decisions map[string]workflow.TaskDecision) (bool, error) {
	if f.phaseErr != nil {
		return false, f.phaseErr
	}
	if workflow.Terminal(f.stored.Phase) {
		// The store contract: terminal bookkeeping fires once. A write to a
		// finished run is refused and reports no change.
		return false, nil
	}
	keys := make([]string, 0, len(decisions))
	for name := range decisions {
		keys = append(keys, name)
	}
	f.phases = append(f.phases, workflowPhaseWrite{runID: runID, phase: phase, keys: keys})
	f.stored.Phase = phase
	return true, nil
}

type fakeWorkflowRevisions struct {
	byID     map[int64]revision.Revision
	active   []revision.Revision
	loadErr  error
	activeEr error
	// workflowActive mirrors the store's contract: ActiveRevisions never
	// returns a workflow-sourced revision (its list feeds the scheduler), so
	// workflow revisions arrive only through ActiveWorkflowRevisions. Keeping
	// the two lists disjoint is what makes the retainer tests honest.
	workflowActive []revision.Revision
	workflowEr     error
}

// ApplyRevisionForTenant satisfies Runtime.Revisions, which shares the
// revision field with the workflow reconcile; the workflow tests never apply.
func (f *fakeWorkflowRevisions) ApplyRevisionForTenant(context.Context, string, revision.Revision) (int64, error) {
	return 0, errors.New("not implemented by this fake")
}

func (f *fakeWorkflowRevisions) RevisionByID(_ context.Context, _ string, id int64) (revision.Revision, error) {
	if f.loadErr != nil {
		return revision.Revision{}, f.loadErr
	}
	rev, ok := f.byID[id]
	if !ok {
		return revision.Revision{}, errors.New("revision not found")
	}
	return rev, nil
}

func (f *fakeWorkflowRevisions) ActiveRevisions(_ context.Context, _ string) ([]revision.Revision, error) {
	if f.activeEr != nil {
		return nil, f.activeEr
	}
	return f.active, nil
}

func (f *fakeWorkflowRevisions) ActiveWorkflowRevisions(_ context.Context, _ string) ([]revision.Revision, error) {
	if f.workflowEr != nil {
		return nil, f.workflowEr
	}
	return f.workflowActive, nil
}

type fakeWorkflowPublisher struct {
	published []v1alpha1.JobRun
	cancels   []string
	err       error
}

func (f *fakeWorkflowPublisher) Publish(_ context.Context, run v1alpha1.JobRun) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, run)
	return nil
}

func (f *fakeWorkflowPublisher) RequestCancel(_ context.Context, _, name string) error {
	if f.err != nil {
		return f.err
	}
	f.cancels = append(f.cancels, name)
	return nil
}

// --- helpers ----------------------------------------------------------------

func workflowRunObject(t *testing.T, mutate func(*v1alpha1.WorkflowRun)) unstructured.Unstructured {
	t.Helper()
	run := v1alpha1.WorkflowRun{
		TypeMeta: metav1.TypeMeta{APIVersion: "workloads.orbitjob.io/v1alpha1", Kind: "WorkflowRun"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "nightly-pipeline-1", Namespace: "finance", UID: "wfr-uid-1",
		},
		Spec: v1alpha1.WorkflowRunSpec{
			WorkflowRef:        v1alpha1.ObjectReference{Name: "nightly-pipeline", UID: "wf-uid-1"},
			DefinitionRevision: 5,
			Trigger:            v1alpha1.Manual,
			Actor:              "principal-key-1",
			OccurrenceKey:      "wocc-1",
		},
	}
	if mutate != nil {
		mutate(&run)
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		t.Fatal(err)
	}
	out := unstructured.Unstructured{Object: obj}
	out.SetGroupVersionKind(workflowRunGVR.GroupVersion().WithKind("WorkflowRun"))
	return out
}

func workflowSpecJSON(t *testing.T, spec v1alpha1.WorkflowJobSpec) string {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func workflowRevision(t *testing.T, id int64, spec string) revision.Revision {
	t.Helper()
	rev, err := revision.New(revision.Identity{
		SourceMode: "workflow", SourceUID: "wf-uid-1", Namespace: "finance", Name: "nightly-pipeline",
	}, 2, spec, "operator", "", time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	rev.ID = id
	return rev
}

func twoTaskSpec() v1alpha1.WorkflowJobSpec {
	return v1alpha1.WorkflowJobSpec{
		Schedule: "0 2 * * *",
		Tasks: []v1alpha1.WorkflowTaskSpec{
			{Name: "extract", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "extract-job"}},
			{Name: "load", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "load-job"}, DependsOn: []string{"extract"}},
		},
	}
}

func newWorkflowRuntime(t *testing.T, revisions *fakeWorkflowRevisions, runs *fakeWorkflowRuns, objs ...runtime.Object) Runtime {
	t.Helper()
	return Runtime{
		Dynamic:   fakeDynamic(t, objs...),
		Revisions: revisions,
		Workflows: runs,
		Tenants:   NamespaceTenantResolver{Tenants: map[string]string{"finance": "tenant-a"}},
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
}

// --- reconcile ---------------------------------------------------------------

// TestReconcileWorkflowRunMaterializesLedgerRow pins the CR-first flow: a
// WorkflowRun with no stored row creates exactly one ledger row carrying the
// CR's declared identity, and projects the run's status back onto the CR.
func TestReconcileWorkflowRunMaterializesLedgerRow(t *testing.T) {
	spec := workflowSpecJSON(t, twoTaskSpec())
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, spec)}}
	runs := &fakeWorkflowRuns{}
	obj := workflowRunObject(t, nil)
	rt := newWorkflowRuntime(t, revs, runs, &obj)

	if err := rt.ReconcileWorkflowRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if len(runs.created) != 1 {
		t.Fatalf("created runs = %d, want exactly 1", len(runs.created))
	}
	created := runs.created[0]
	if created.SourceUID != "wf-uid-1" || created.OccurrenceKey != "wocc-1" ||
		created.RevisionID != 5 || created.Trigger != "Manual" || created.Actor != "principal-key-1" ||
		created.Phase != workflow.PhasePending {
		t.Fatalf("created run = %+v, want the CR's declared identity at Pending", created)
	}
}

// TestReconcileWorkflowRunIsIdempotentOnRedelivery pins convergence: a
// re-delivered CR finds the stored row, materializes nothing, and re-projects
// the same status.
func TestReconcileWorkflowRunIsIdempotentOnRedelivery(t *testing.T) {
	spec := workflowSpecJSON(t, twoTaskSpec())
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, spec)}}
	stored := workflow.Run{
		ID: 42, TenantID: "tenant-a", SourceUID: "wf-uid-1", RevisionID: 5,
		OccurrenceKey: "wocc-1", Trigger: "Manual", Actor: "principal-key-1",
		Phase: workflow.PhaseRunning, CreatedAt: time.Unix(1699999000, 0).UTC(),
	}
	runs := &fakeWorkflowRuns{stored: stored, found: true}
	obj := workflowRunObject(t, nil)
	rt := newWorkflowRuntime(t, revs, runs, &obj)

	for i := 0; i < 2; i++ {
		if err := rt.ReconcileWorkflowRun(context.Background(), obj); err != nil {
			t.Fatalf("delivery %d: %v", i+1, err)
		}
	}
	if len(runs.created) != 0 {
		t.Fatalf("created runs = %d, want 0: a re-delivered CR must not re-materialize", len(runs.created))
	}
}

// TestReconcileWorkflowRunRefusesForgedPins refuses the CRs that would invent
// work: a run whose pinned revision does not exist, and one whose revision
// belongs to a different workflow.
func TestReconcileWorkflowRunRefusesForgedPins(t *testing.T) {
	t.Run("unknown revision", func(t *testing.T) {
		revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{}}
		runs := &fakeWorkflowRuns{}
		obj := workflowRunObject(t, nil)
		rt := newWorkflowRuntime(t, revs, runs, &obj)
		err := rt.ReconcileWorkflowRun(context.Background(), obj)
		if err == nil {
			t.Fatal("expected refusal for a run whose revision cannot be loaded")
		}
	})
	t.Run("revision of another workflow", func(t *testing.T) {
		spec := workflowSpecJSON(t, twoTaskSpec())
		revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, spec)}}
		runs := &fakeWorkflowRuns{}
		obj := workflowRunObject(t, func(r *v1alpha1.WorkflowRun) {
			r.Spec.WorkflowRef.UID = "wf-uid-other"
		})
		rt := newWorkflowRuntime(t, revs, runs, &obj)
		err := rt.ReconcileWorkflowRun(context.Background(), obj)
		if err == nil {
			t.Fatal("expected refusal for a revision/reference identity mismatch")
		}
		if len(runs.created) != 0 {
			t.Fatalf("created runs = %d, want 0: a forged CR must not reach the ledger", len(runs.created))
		}
	})
}

// TestReconcileWorkflowRunRefusesInvalidDAG refuses to materialize a run whose
// pinned revision decodes to a cyclic DAG: admission belongs to the projection
// boundary, and a run that cannot be walked must not exist.
func TestReconcileWorkflowRunRefusesInvalidDAG(t *testing.T) {
	cyclic := v1alpha1.WorkflowJobSpec{
		Schedule: "0 2 * * *",
		Tasks: []v1alpha1.WorkflowTaskSpec{
			{Name: "a", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "j"}, DependsOn: []string{"b"}},
			{Name: "b", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "j"}, DependsOn: []string{"a"}},
		},
	}
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, workflowSpecJSON(t, cyclic))}}
	runs := &fakeWorkflowRuns{}
	obj := workflowRunObject(t, nil)
	rt := newWorkflowRuntime(t, revs, runs, &obj)
	err := rt.ReconcileWorkflowRun(context.Background(), obj)
	if err == nil {
		t.Fatal("expected refusal for an invalid DAG")
	}
	if len(runs.created) != 0 {
		t.Fatalf("created runs = %d, want 0", len(runs.created))
	}
}

// TestReconcileWorkflowRunRecordsCancelIntent pins the stop path: a
// cancelRequested CR marks the open ledger row CancelRequested so the walker
// stops advancing it, and a terminal row is never dragged back.
func TestReconcileWorkflowRunRecordsCancelIntent(t *testing.T) {
	spec := workflowSpecJSON(t, twoTaskSpec())
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, spec)}}

	t.Run("open run is marked CancelRequested", func(t *testing.T) {
		runs := &fakeWorkflowRuns{stored: workflow.Run{
			ID: 42, SourceUID: "wf-uid-1", RevisionID: 5, OccurrenceKey: "wocc-1",
			Phase: workflow.PhaseRunning,
		}, found: true}
		obj := workflowRunObject(t, func(r *v1alpha1.WorkflowRun) { r.Spec.CancelRequested = true })
		rt := newWorkflowRuntime(t, revs, runs, &obj)
		if err := rt.ReconcileWorkflowRun(context.Background(), obj); err != nil {
			t.Fatal(err)
		}
		if len(runs.phases) != 1 || runs.phases[0].phase != workflow.PhaseCancelRequested {
			t.Fatalf("phase writes = %+v, want one CancelRequested", runs.phases)
		}
	})
	t.Run("terminal run is left alone", func(t *testing.T) {
		runs := &fakeWorkflowRuns{stored: workflow.Run{
			ID: 42, SourceUID: "wf-uid-1", RevisionID: 5, OccurrenceKey: "wocc-1",
			Phase: workflow.PhaseSucceeded,
		}, found: true}
		obj := workflowRunObject(t, func(r *v1alpha1.WorkflowRun) { r.Spec.CancelRequested = true })
		rt := newWorkflowRuntime(t, revs, runs, &obj)
		if err := rt.ReconcileWorkflowRun(context.Background(), obj); err != nil {
			t.Fatal(err)
		}
		if len(runs.phases) != 0 {
			t.Fatalf("phase writes = %+v, want none: terminal bookkeeping fires once", runs.phases)
		}
	})
}

// TestReconcileWorkflowRunProjectsStepStatus pins the read model: the CR
// status carries one entry per task, with the step's facts for a task that
// ran and the skip decision for one that did not.
func TestReconcileWorkflowRunProjectsStepStatus(t *testing.T) {
	spec := workflowSpecJSON(t, twoTaskSpec())
	revs := &fakeWorkflowRevisions{byID: map[int64]revision.Revision{5: workflowRevision(t, 5, spec)}}
	stored := workflow.Run{
		ID: 42, SourceUID: "wf-uid-1", RevisionID: 5, OccurrenceKey: "wocc-1",
		Phase:         workflow.PhaseRunning,
		TaskDecisions: map[string]workflow.TaskDecision{},
	}
	extractKey := workflow.StepOccurrenceKey("wf-uid-1", "wocc-1", "extract")
	runs := &fakeWorkflowRuns{stored: stored, found: true, steps: []workflow.StepRun{{
		ID: 7, WorkflowRunID: 42, SourceUID: "extract-job-uid", OccurrenceKey: extractKey,
		Trigger: "Workflow", Phase: "Running", Attempt: 1,
	}}}
	rt := newWorkflowRuntime(t, revs, runs)

	obj := workflowRunObject(t, nil)
	status, err := rt.workflowStatus(context.Background(), "tenant-a", runs.stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Steps) != 2 {
		t.Fatalf("status steps = %d, want one per task", len(status.Steps))
	}
	if status.Steps[0].Task != "extract" || status.Steps[0].Phase != "Running" || status.Steps[0].OccurrenceKey != extractKey {
		t.Fatalf("extract step = %+v, want the stored step's facts", status.Steps[0])
	}
	if status.Steps[1].Task != "load" || status.Steps[1].Skipped {
		t.Fatalf("load step = %+v, want neither a step nor a decision yet", status.Steps[1])
	}
	_ = obj
}
