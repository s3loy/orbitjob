package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/revision"
)

type fakeFunctionRecorder struct {
	records []function.CompletedRecord
	err     error
}

func (f *fakeFunctionRecorder) RecordCompleted(_ context.Context, _ string, record function.CompletedRecord) error {
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, record)
	return nil
}

type fakeTerminalOutcomes struct {
	outcome controlplane.TerminalOutcome
	err     error
	calls   int
}

func (f *fakeTerminalOutcomes) TerminalOutcome(_ context.Context, _ string, _ int64) (controlplane.TerminalOutcome, error) {
	f.calls++
	return f.outcome, f.err
}

func functionRunObject(t *testing.T) unstructured.Unstructured {
	t.Helper()
	run := v1alpha1.JobRun{
		TypeMeta: metav1.TypeMeta{APIVersion: "workloads.orbitjob.io/v1alpha1", Kind: "JobRun"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "resize-image-a1b2c3d4", Namespace: "finance", UID: "fnrun-uid-1",
		},
		Spec: v1alpha1.JobRunSpec{
			ScheduledJobRef:    v1alpha1.ObjectReference{Name: "resize-image", UID: "function-9"},
			DefinitionRevision: 3,
			Trigger:            v1alpha1.Function,
			Actor:              "principal-key-1",
			OccurrenceKey:      "f0e1d2c3a4b5c6d7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1",
		},
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&run)
	if err != nil {
		t.Fatal(err)
	}
	out := unstructured.Unstructured{Object: obj}
	out.SetGroupVersionKind(jobRunGVR.GroupVersion().WithKind("JobRun"))
	return out
}

// TestMaterializeManualRunAcceptsFunctionTrigger pins the materialize gate: a
// Function-trigger CR with no stored run is materialized exactly like a
// manual one — CR-first, deduplicated on the occurrence key — while a forged
// non-CR-first trigger still is not.
func TestMaterializeManualRunAcceptsFunctionTrigger(t *testing.T) {
	rev := withIdentity(t, revisionFor(t, 3, `{"schedule":"* * * * *","jobTemplate":{"image":"registry.example.com/fn:v1"}}`),
		"function", "function-9", "finance", "resize-image")
	revs := &fakeRevisions{byID: map[int64]revision.Revision{3: rev}}
	runs := &fakeRuns{}
	obj := functionRunObject(t)
	rt := newRuntime(t, fakeDynamic(t, &obj), revs, runs, &fakeJobs{})

	_, found, err := rt.materializeManualRun(context.Background(), "tenant-a", v1alpha1.JobRunSpec{
		ScheduledJobRef:    v1alpha1.ObjectReference{Name: "resize-image", UID: "function-9"},
		DefinitionRevision: 3,
		Trigger:            v1alpha1.Function,
		Actor:              "principal-key-1",
		OccurrenceKey:      "f0e1d2c3",
	})
	if err != nil || !found {
		t.Fatalf("found=%v err=%v, want the function run materialized", found, err)
	}
	if len(runs.createdRuns) != 1 || runs.createdRuns[0].Trigger != jobrun.Function ||
		runs.createdRuns[0].SourceUID != "function-9" || runs.createdRuns[0].OccurrenceKey != "f0e1d2c3" {
		t.Fatalf("created runs = %+v, want one Function run of function-9 deduplicated on the occurrence key", runs.createdRuns)
	}
	if len(runs.attempts) != 0 {
		t.Fatalf("materialization must not start attempts")
	}

	// A Workflow-trigger CR with no step row is still not materialized: the
	// walker commits the row first, so a bare CR is forged, and the run
	// reconciliation turns the not-found into its refusal.
	before := len(runs.createdRuns)
	_, found, err = rt.materializeManualRun(context.Background(), "tenant-a", v1alpha1.JobRunSpec{
		ScheduledJobRef:    v1alpha1.ObjectReference{Name: "resize-image", UID: "function-9"},
		DefinitionRevision: 3,
		Trigger:            v1alpha1.Workflow,
		Actor:              ActorWorkflowWalker,
		OccurrenceKey:      "wocc-9",
	})
	if found || err != nil || len(runs.createdRuns) != before {
		t.Fatalf("found=%v err=%v created=%d, want no materialization for a Workflow-trigger CR", found, err, len(runs.createdRuns))
	}
}

// TestRecordFunctionOutcomeRoutesFunctionRunsOnly pins the hook: a terminal
// transition whose source uid carries the function prefix upserts the read
// model with the derived run id and mapped status; any other definition's
// outcome reaches nothing, and a replayed transition reaches nothing.
func TestRecordFunctionOutcomeRoutesFunctionRunsOnly(t *testing.T) {
	outcome := controlplane.TerminalOutcome{
		RunID:         11,
		SourceUID:     "function-9",
		OccurrenceKey: "f0e1d2c3",
		Phase:         jobrun.Succeeded,
		StartedAt:     time.Unix(1700000000, 0).UTC(),
		CompletedAt:   time.Unix(1700000065, 0).UTC(),
	}
	functions := &fakeFunctionRecorder{}
	outcomes := &fakeTerminalOutcomes{outcome: outcome}
	rt := Runtime{
		Functions:     functions,
		OutcomeReader: outcomes,
		Now:           func() time.Time { return time.Unix(1700000100, 0).UTC() },
	}

	rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Succeeded, true)
	if len(functions.records) != 1 {
		t.Fatalf("records = %+v, want exactly one upsert", functions.records)
	}
	record := functions.records[0]
	if record.FunctionID != 9 || record.Status != function.StatusSuccess {
		t.Fatalf("record = %+v, want function 9 as success", record)
	}
	if record.RunID != functionRunID("f0e1d2c3") {
		t.Fatalf("run id = %s, want the deterministic derivation", record.RunID)
	}
	if record.DurationMs != 65000 {
		t.Fatalf("duration = %d, want 65000 from the attempt span", record.DurationMs)
	}
	if record.TriggeredAt != outcome.StartedAt {
		t.Fatalf("triggered at = %s, want the attempt start", record.TriggeredAt)
	}

	functions.records = nil
	rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Succeeded, false)
	if len(functions.records) != 0 {
		t.Fatalf("records = %+v, want none: a replayed transition must not double-write", functions.records)
	}

	outcomesNonFunction := &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{
		RunID: 11, SourceUID: "a-uuid-not-a-function", OccurrenceKey: "occ-1", Phase: jobrun.Succeeded,
	}}
	rt.OutcomeReader = outcomesNonFunction
	rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Succeeded, true)
	if len(functions.records) != 0 {
		t.Fatalf("records = %+v, want none for a non-function definition", functions.records)
	}

	rt.OutcomeReader = &fakeTerminalOutcomes{outcome: outcome}
	rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Canceled, true)
	if len(functions.records) != 1 || functions.records[0].Status != function.StatusCanceled {
		t.Fatalf("records = %+v, want the canceled outcome recorded", functions.records)
	}
}

// TestRecordFunctionOutcomeToleratesReadFailures pins the checkobserve call:
// the phase write is the fact, so a failed outcome read or a failed read
// model write is absorbed, never failed over into the reconcile.
func TestRecordFunctionOutcomeToleratesReadFailures(t *testing.T) {
	rt := Runtime{
		Functions:     &fakeFunctionRecorder{},
		OutcomeReader: &fakeTerminalOutcomes{err: errors.New("read failed")},
	}
	rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Failed, true)

	rt.OutcomeReader = &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{SourceUID: "function-9", OccurrenceKey: "occ"}}
	rt.Functions = &fakeFunctionRecorder{err: errors.New("upsert failed")}
	rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Failed, true)
}

// TestCancelOfFunctionRunRecordsCanceledOutcome pins the cancel path's hook:
// a function invocation whose stop is confirmed lands in the read model with
// the canceled status, exactly once.
func TestCancelOfFunctionRunRecordsCanceledOutcome(t *testing.T) {
	rev := withIdentity(t, revisionFor(t, 7, `{"schedule":"* * * * *","jobTemplate":{"image":"registry.example.com/fn:v1"}}`),
		"function", "function-4", "finance", "resize-image")
	revs := &fakeRevisions{byID: map[int64]revision.Revision{7: rev}}
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	// reportPhaseChange=true models the real transition to Canceled; the
	// hook fires only on genuine transitions.
	runs := &fakeRuns{found: true, stored: stored, reportPhaseChange: true}
	jobs := &fakeJobs{}
	functions := &fakeFunctionRecorder{}
	rt := newRuntime(t, fakeDynamic(t, &obj), revs, runs, jobs)
	rt.OutcomeReader = &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{
		RunID: 11, SourceUID: "function-4", OccurrenceKey: "f0e1d2c3", Phase: jobrun.Canceled,
	}}
	rt.Functions = functions

	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if last := runs.phases[len(runs.phases)-1]; last.phase != jobrun.Canceled {
		t.Fatalf("final phase = %+v, want Canceled", runs.phases)
	}
	if len(functions.records) != 1 || functions.records[0].Status != function.StatusCanceled || functions.records[0].FunctionID != 4 {
		t.Fatalf("records = %+v, want one canceled row for function 4", functions.records)
	}
}
