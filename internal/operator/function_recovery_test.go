package operator

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/jobrun"
)

// The ledger commit survives a failed derived-history write, as in PostgreSQL.
type committingFunctionRuns struct{ *fakeRuns }

func (f *committingFunctionRuns) UpdateRunPhase(ctx context.Context, tenant string, id int64, phase jobrun.Phase, attempt int) (bool, error) {
	_, err := f.fakeRuns.UpdateRunPhase(ctx, tenant, id, phase, attempt)
	if err != nil {
		return false, err
	}
	changed := f.stored.Phase != phase || f.stored.Attempt != attempt
	f.stored.Phase, f.stored.Attempt = phase, attempt
	return changed, nil
}

func TestFunctionHistoryRecoversAfterTerminalCommitWithoutAnotherTransition(t *testing.T) {
	for _, terminal := range []jobrun.Phase{jobrun.Succeeded, jobrun.Failed, jobrun.Canceled} {
		t.Run(string(terminal), func(t *testing.T) {
			ctx := context.Background()
			obj := functionRunObject(t)
			initial := jobrun.Running
			attempt := 1
			if terminal == jobrun.Failed {
				initial = jobrun.RetryWaiting
			}
			if terminal == jobrun.Canceled {
				initial, attempt = jobrun.Pending, 0
				if err := unstructured.SetNestedField(obj.Object, true, "spec", "cancelRequested"); err != nil {
					t.Fatal(err)
				}
			}
			runs := &committingFunctionRuns{&fakeRuns{found: true, stored: jobrun.StoredRun{
				ID: 11, RevisionID: 3, Phase: initial, Attempt: attempt, MaxAttempts: 1,
			}, observedRun: 11, observedNum: 1}}
			recorder := &fakeFunctionRecorder{failures: 1}
			outcome := controlplane.TerminalOutcome{RunID: 11, SourceUID: "function-9", OccurrenceKey: "f0e1d2c3", Phase: terminal,
				CreatedAt: time.Unix(1699999900, 0).UTC(), CompletedAt: time.Unix(1700000000, 0).UTC()}
			if terminal != jobrun.Canceled {
				outcome.StartedAt = outcome.CompletedAt.Add(-time.Second)
			}
			rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
			rt.Functions = recorder
			rt.OutcomeReader = &fakeTerminalOutcomes{outcome: outcome}
			now := outcome.CompletedAt.Add(time.Minute)
			rt.Now = func() time.Time { return now }
			var err error
			if terminal == jobrun.Succeeded {
				err = rt.ReconcileJob(ctx, observedJob(t, "oj-function-9-1", "Succeeded"))
			} else {
				err = rt.ReconcileJobRun(ctx, obj)
			}
			if err == nil || !strings.Contains(err.Error(), "history database unavailable") {
				t.Fatalf("write failure did not reach controller: %v", err)
			}
			if runs.stored.Phase != terminal || len(recorder.records) != 0 {
				t.Fatalf("terminal ledger must survive without invented history: phase=%s records=%+v", runs.stored.Phase, recorder.records)
			}
			transitions := len(runs.phases)
			for replay := 0; replay < 2; replay++ {
				now = now.Add(time.Minute)
				if err := rt.ReconcileJobRun(ctx, obj); err != nil {
					t.Fatalf("terminal replay %d: %v", replay, err)
				}
			}
			if len(recorder.records) != 1 || recorder.calls != 3 || len(runs.phases) != transitions || len(runs.attempts) != 0 {
				t.Fatalf("replay must repair one row without another transition or attempt: records=%+v calls=%d phases=%+v", recorder.records, recorder.calls, runs.phases)
			}
			status, _ := function.ReadModelStatus(terminal)
			if recorder.records[0].Status != status {
				t.Fatalf("history outcome = %s, want %s", recorder.records[0].Status, status)
			}
			if recorder.records[0].TriggeredAt != outcome.CreatedAt {
				t.Fatalf("history repair moved trigger time to %s, want ledger creation %s", recorder.records[0].TriggeredAt, outcome.CreatedAt)
			}
			if terminal == jobrun.Canceled && !recorder.records[0].StartedAt.IsZero() {
				t.Fatal("canceled-before-start acquired a fabricated start")
			}
		})
	}
}

func TestTerminalOrdinaryJobRunDoesNotEnterFunctionRepair(t *testing.T) {
	obj := jobRunObject(t)
	runs := &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 11, RevisionID: 7, Phase: jobrun.Succeeded, Attempt: 1}}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
	reader := &fakeTerminalOutcomes{}
	recorder := &fakeFunctionRecorder{}
	rt.OutcomeReader, rt.Functions = reader, recorder
	if err := rt.ReconcileJobRun(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 0 || recorder.calls != 0 {
		t.Fatal("ordinary terminal JobRun was treated as a Function")
	}
}

func TestHistoryWriteFailureDoesNotHideRecordedTerminalStatus(t *testing.T) {
	obj := functionRunObject(t)
	runs := &fakeRuns{found: true, stored: jobrun.StoredRun{ID: 11, RevisionID: 3, Phase: jobrun.Canceled}}
	rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
	rt.Functions = &fakeFunctionRecorder{failures: 1}
	rt.OutcomeReader = &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{
		SourceUID: "function-9", OccurrenceKey: "occ", Phase: jobrun.Canceled,
	}}
	if err := rt.ReconcileJobRun(context.Background(), obj); err == nil {
		t.Fatal("history write failure did not request controller retry")
	}
	updated, err := rt.Dynamic.Resource(jobRunGVR).Namespace(obj.GetNamespace()).Get(context.Background(), obj.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	phase, _, _ := unstructured.NestedString(updated.Object, "status", "phase")
	if phase != string(jobrun.Canceled) {
		t.Fatalf("confirmed ledger phase was hidden by derived-history failure: %s", phase)
	}
}

func TestFunctionHistoryUsesCommittedOutcomeInsteadOfStaleObservation(t *testing.T) {
	recorder := &fakeFunctionRecorder{}
	rt := Runtime{Functions: recorder, OutcomeReader: &fakeTerminalOutcomes{outcome: controlplane.TerminalOutcome{
		SourceUID: "function-9", OccurrenceKey: "occ", Phase: jobrun.Canceled,
	}}}
	if err := rt.recordFunctionOutcome(context.Background(), "tenant-a", 11, jobrun.Succeeded); err != nil {
		t.Fatal(err)
	}
	if len(recorder.records) != 1 || recorder.records[0].Status != function.StatusCanceled {
		t.Fatal("stale observation replaced the recorded terminal outcome")
	}
}
