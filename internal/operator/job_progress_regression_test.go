package operator

import (
	"context"
	"orbitjob/internal/core/domain/jobrun"
	"testing"
)

type phaseCapture struct {
	*fakeRuns
	attemptPhase string
}

func (s *phaseCapture) UpdateAttemptPhase(_ context.Context, _, _, phase, _, _ string) (int64, int, error) {
	s.attemptPhase = phase
	return 42, 1, nil
}
func TestFailedJobCompletesAttemptAndWakesRun(t *testing.T) {
	runs := &phaseCapture{fakeRuns: &fakeRuns{reportPhaseChange: true}}
	rt := Runtime{Runs: runs, Tenants: NamespaceTenantResolver{Tenants: map[string]string{"finance": executionTenant}}}
	var keys []string
	rt.Enqueue = func(resource, namespace, name string) { keys = append(keys, resource+":"+namespace+"/"+name) }
	if err := rt.ReconcileJob(context.Background(), observedJob(t, "oj-nightly-report-1-1", "Failed")); err != nil {
		t.Fatal(err)
	}
	if runs.attemptPhase != string(jobrun.Failed) {
		t.Fatalf("completed Job recorded nonterminal attempt phase %s", runs.attemptPhase)
	}
	if len(runs.phases) != 1 || runs.phases[0].phase != jobrun.RetryWaiting {
		t.Fatalf("run must await retry decision: %+v", runs.phases)
	}
	if len(keys) != 1 || keys[0] != "jobruns:finance/nightly-report-1" {
		t.Fatalf("JobRun never awakened: %v", keys)
	}
}
