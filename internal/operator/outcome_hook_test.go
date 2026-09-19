package operator

import (
	"context"
	"testing"

	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
)

type recordingOutcomes struct {
	calls []outcomeCall
}

type outcomeCall struct {
	tenant string
	runID  int64
	phase  jobrun.Phase
}

func (r *recordingOutcomes) RecordTerminalPhase(_ context.Context, tenantID string, runID int64, phase jobrun.Phase) {
	r.calls = append(r.calls, outcomeCall{tenant: tenantID, runID: runID, phase: phase})
}

// TestReconcileJobHandsTerminalTransitionToOutcomes pins the hook contract: a
// real transition to Succeeded reaches the recorder exactly once, a resync
// replay of an already-stored phase reaches nothing, and a non-terminal phase
// (RetryWaiting) reaches nothing even though the write changed the row.
func TestReconcileJobHandsTerminalTransitionToOutcomes(t *testing.T) {
	t.Run("a real Succeeded transition is handed over once", func(t *testing.T) {
		obj := observedJob(t, "oj-nightly-report-1-1", execution.PhaseSucceeded)
		runs := &fakeRuns{observedRun: 11, observedNum: 1, reportPhaseChange: true}
		hook := &recordingOutcomes{}
		rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
		rt.Outcomes = hook

		if err := rt.ReconcileJob(context.Background(), obj); err != nil {
			t.Fatal(err)
		}
		if len(hook.calls) != 1 || hook.calls[0] != (outcomeCall{tenant: "tenant-a", runID: 11, phase: jobrun.Succeeded}) {
			t.Fatalf("outcome calls = %+v, want one Succeeded handover", hook.calls)
		}
	})

	t.Run("a resync replay of stored state is silent", func(t *testing.T) {
		obj := observedJob(t, "oj-nightly-report-1-1", execution.PhaseSucceeded)
		runs := &fakeRuns{observedRun: 11, observedNum: 1, reportPhaseChange: false}
		hook := &recordingOutcomes{}
		rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
		rt.Outcomes = hook

		if err := rt.ReconcileJob(context.Background(), obj); err != nil {
			t.Fatal(err)
		}
		if len(hook.calls) != 0 {
			t.Fatalf("outcome calls = %+v, want none: replays must not double-count", hook.calls)
		}
	})

	t.Run("RetryWaiting is not an outcome", func(t *testing.T) {
		obj := observedJob(t, "oj-nightly-report-1-1", execution.PhaseFailed)
		runs := &fakeRuns{observedRun: 11, observedNum: 1, reportPhaseChange: true}
		hook := &recordingOutcomes{}
		rt := newRuntime(t, fakeDynamic(t, &obj), &fakeRevisions{}, runs, &fakeJobs{})
		rt.Outcomes = hook

		if err := rt.ReconcileJob(context.Background(), obj); err != nil {
			t.Fatal(err)
		}
		if len(hook.calls) != 0 {
			t.Fatalf("outcome calls = %+v, want none until attempts are spent", hook.calls)
		}
	})
}
