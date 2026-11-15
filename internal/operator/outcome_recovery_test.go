package operator

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
)

type pendingRuns struct {
	*fakeRuns
	pending []controlplane.TerminalOutcome
	marked  int
}

func (p *pendingRuns) TerminalOutcomesPendingAfter(_ context.Context, _ string, after int64, limit int) ([]controlplane.TerminalOutcome, error) {
	var result []controlplane.TerminalOutcome
	for _, outcome := range p.pending {
		if outcome.RunID > after && len(result) < limit {
			result = append(result, outcome)
		}
	}
	return result, nil
}
func (p *pendingRuns) MarkOutcomeRecorded(context.Context, string, int64) error {
	p.marked++
	p.pending = nil
	return nil
}

type recoveringRecorder struct {
	fail  bool
	calls int
}

func (r *recoveringRecorder) RecordTerminalPhase(context.Context, string, int64, jobrun.Phase) {}
func (r *recoveringRecorder) RecordTerminalPhaseOnce(context.Context, string, int64, jobrun.Phase) error {
	r.calls++
	if r.fail {
		return errors.New("derived write interrupted")
	}
	return nil
}
func TestTerminalOutcomesReplayOnlyAcknowledgesCompletedBookkeeping(t *testing.T) {
	ledger := &pendingRuns{fakeRuns: &fakeRuns{}, pending: []controlplane.TerminalOutcome{{RunID: 42, Phase: jobrun.Succeeded}}}
	rec := &recoveringRecorder{fail: true}
	rt := Runtime{Runs: ledger, Outcomes: rec}
	cursor := &OutcomeReplayCursor{}
	if _, err := rt.ReplayOutcomes(context.Background(), []string{executionTenant}, 100, cursor); err == nil {
		t.Fatal("failed derived write must remain pending")
	}
	if ledger.marked != 0 {
		t.Fatal("failed outcome was acknowledged")
	}
	rec.fail = false
	if n, err := rt.ReplayOutcomes(context.Background(), []string{executionTenant}, 100, cursor); err != nil || n != 1 {
		t.Fatalf("recovery count=%d error=%v", n, err)
	}
	if ledger.marked != 1 || rec.calls != 2 {
		t.Fatalf("marked=%d calls=%d", ledger.marked, rec.calls)
	}
	if n, err := rt.ReplayOutcomes(context.Background(), []string{executionTenant}, 100, cursor); err != nil || n != 0 {
		t.Fatalf("acknowledged outcome replayed: %d %v", n, err)
	}
}
