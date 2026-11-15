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
	records  []function.CompletedRecord
	err      error
	failures int
	calls    int
}

func (f *fakeFunctionRecorder) RecordCompleted(_ context.Context, _ string, record function.CompletedRecord) error {
	f.calls++
	if f.failures > 0 {
		f.failures--
		return errors.New("history database unavailable")
	}
	if f.err != nil {
		return f.err
	}
	for _, existing := range f.records {
		if existing.RunID == record.RunID {
			return nil
		}
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
			Actor:              executionActorKeyID,
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

	_, found, err := rt.materializeManualRun(context.Background(), executionTenant, v1alpha1.JobRunSpec{
		ScheduledJobRef:    v1alpha1.ObjectReference{Name: "resize-image", UID: "function-9"},
		DefinitionRevision: 3,
		Trigger:            v1alpha1.Function,
		Actor:              executionActorKeyID,
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
	_, found, err = rt.materializeManualRun(context.Background(), executionTenant, v1alpha1.JobRunSpec{
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
// outcome reaches nothing, and a replay keeps one history row.
func TestRecordFunctionOutcomeRoutesFunctionRunsOnly(t *testing.T) {
	outcome := controlplane.TerminalOutcome{
		RunID:         11,
		SourceUID:     "function-9",
		OccurrenceKey: "f0e1d2c3",
		Phase:         jobrun.Succeeded,
		CreatedAt:     time.Unix(1699999900, 0).UTC(),
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

	if err := rt.recordFunctionOutcome(context.Background(), executionTenant, 11, jobrun.Succeeded, true); err != nil {
		t.Fatal(err)
	}
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
	if record.TriggeredAt != outcome.CreatedAt {
		t.Fatalf("triggered at = %s, want ledger creation %s", record.TriggeredAt, outcome.CreatedAt)
	}
	if record.StartedAt != outcome.StartedAt {
		t.Fatalf("started at = %s, want the newest attempt start", record.StartedAt)
	}

	functions.records = nil
	if err := rt.recordFunctionOutcome(context.Background(), executionTenant, 11, jobrun.Succeeded, false); err != nil {
		t.Fatal(err)
	}
	if len(functions.records) != 0 {
		t.Fatalf("records = %+v, want none: a replayed transition must not double-write", functions.records)
	}
	functions.records = nil

	outcomesNonFunction := &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{
		RunID: 11, SourceUID: "a-uuid-not-a-function", OccurrenceKey: "occ-1", Phase: jobrun.Succeeded,
	}}
	rt.OutcomeReader = outcomesNonFunction
	if err := rt.recordFunctionOutcome(context.Background(), executionTenant, 11, jobrun.Succeeded, true); err != nil {
		t.Fatal(err)
	}
	if len(functions.records) != 0 {
		t.Fatalf("records = %+v, want none for a non-function definition", functions.records)
	}

	outcome.Phase = jobrun.Canceled
	rt.OutcomeReader = &fakeTerminalOutcomes{outcome: outcome}
	if err := rt.recordFunctionOutcome(context.Background(), executionTenant, 11, jobrun.Canceled, true); err != nil {
		t.Fatal(err)
	}
	if len(functions.records) != 1 || functions.records[0].Status != function.StatusCanceled {
		t.Fatalf("records = %+v, want the canceled outcome recorded", functions.records)
	}
}

func TestRecordFunctionOutcomeRetainsLegacyReaderTimingFallback(t *testing.T) {
	now := time.Unix(1700000100, 0).UTC()
	started := now.Add(-time.Minute)
	for _, hasAttempt := range []bool{true, false} {
		name := "attempt start"
		if !hasAttempt {
			name = "clock without attempt"
		}
		t.Run(name, func(t *testing.T) {
			outcome := controlplane.TerminalOutcome{
				SourceUID: "function-9", OccurrenceKey: "legacy", Phase: jobrun.Canceled,
				CompletedAt: now,
			}
			want := now
			if hasAttempt {
				outcome.StartedAt, want = started, started
			}
			recorder := &fakeFunctionRecorder{}
			rt := Runtime{
				Functions: recorder, OutcomeReader: &fakeTerminalOutcomes{outcome: outcome},
				Now: func() time.Time { return now },
			}
			if err := rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Canceled); err != nil {
				t.Fatal(err)
			}
			if len(recorder.records) != 1 || recorder.records[0].TriggeredAt != want {
				t.Fatalf("legacy reader fallback = %+v, want trigger time %s", recorder.records, want)
			}
		})
	}
}

// Errors must reach the controller so the committed terminal fact can be replayed.
func TestRecordFunctionOutcomeReturnsReadAndWriteFailures(t *testing.T) {
	readErr, writeErr := errors.New("read failed"), errors.New("upsert failed")
	rt := Runtime{
		Functions:     &fakeFunctionRecorder{},
		OutcomeReader: &fakeTerminalOutcomes{err: readErr},
	}
	if err := rt.recordFunctionOutcome(context.Background(), executionTenant, 11, jobrun.Failed, true); !errors.Is(err, readErr) {
		t.Fatalf("read error was lost: %v", err)
	}

	rt.OutcomeReader = &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{SourceUID: "function-9", OccurrenceKey: "occ", Phase: jobrun.Failed}}
	rt.Functions = &fakeFunctionRecorder{err: writeErr}
	if err := rt.recordFunctionOutcome(context.Background(), executionTenant, 11, jobrun.Failed, true); !errors.Is(err, writeErr) {
		t.Fatalf("write error was lost: %v", err)
	}
}

// TestCancelOfFunctionRunRecordsCanceledOutcome pins the cancel path's hook:
// a function invocation whose stop is confirmed lands in the read model with
// the canceled status, exactly once.
func TestCancelOfFunctionRunRecordsCanceledOutcome(t *testing.T) {
	rev := withIdentity(t, revisionFor(t, 7, `{"schedule":"* * * * *","jobTemplate":{"image":"registry.example.com/fn:v1"}}`),
		"function", "function-4", "finance", "resize-image")
	revs := &fakeRevisions{byID: map[int64]revision.Revision{7: rev}}
	obj, stored := jobRunObjectWithCancel(t, string(jobrun.Running), 1)
	// The first reconciliation observes the real transition to Canceled.
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

func (f *fakeFunctionRecorder) RecordCompletedOnce(ctx context.Context, tenant string, record function.CompletedRecord) (bool, error) {
	err := f.RecordCompleted(ctx, tenant, record)
	return err == nil, err
}
