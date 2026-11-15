package operator

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
)

type outcomeRecoveryStore interface {
	TerminalOutcomesPendingAfter(context.Context, string, int64, int) ([]controlplane.TerminalOutcome, error)
	MarkOutcomeRecorded(context.Context, string, int64) error
}

// recordTerminal acknowledges only after all idempotent derivatives succeeded.
// Cancellation is not an SLI event, but is a function invocation outcome.
func (r Runtime) recordTerminal(ctx context.Context, tenant string, runID int64, phase jobrun.Phase) error {
	if !jobrun.Terminal(phase) {
		return nil
	}
	if r.Outcomes != nil && (phase == jobrun.Succeeded || phase == jobrun.Failed) {
		if err := r.Outcomes.RecordTerminalPhaseOnce(ctx, tenant, runID, phase); err != nil {
			return err
		}
	}
	if err := r.recordFunctionOutcomeOnce(ctx, tenant, runID, phase); err != nil {
		return err
	}
	if ledger, ok := r.Runs.(outcomeRecoveryStore); ok {
		return ledger.MarkOutcomeRecorded(ctx, tenant, runID)
	}
	return nil
}

// OutcomeReplayCursor rotates each tenant's bounded page across calls. It is
// only scan progress: failed outcomes remain durable and unacknowledged. A
// restart begins at zero and retries them, which is safe for idempotent writers.
// Share one cursor for successive calls of a recovery loop.
type OutcomeReplayCursor struct {
	mu    sync.Mutex
	after map[string]int64
}

// ReplayOutcomes repairs crash windows independently of an informer transition.
// Failed rows cannot monopolize every page: the next call starts after the last
// visited ID, and wraps when it reaches the end of the pending set.
func (r Runtime) ReplayOutcomes(ctx context.Context, tenants []string, limit int, cursor *OutcomeReplayCursor) (int, error) {
	ledger, ok := r.Runs.(outcomeRecoveryStore)
	if !ok {
		return 0, fmt.Errorf("terminal outcome recovery store required")
	}
	if cursor == nil {
		return 0, fmt.Errorf("terminal outcome replay cursor required")
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	if cursor.after == nil {
		cursor.after = make(map[string]int64)
	}
	count := 0
	var failures error
	for _, tenant := range tenants {
		after := cursor.after[tenant]
		pending, err := ledger.TerminalOutcomesPendingAfter(ctx, tenant, after, limit)
		if err == nil && len(pending) == 0 && after != 0 {
			cursor.after[tenant] = 0
			pending, err = ledger.TerminalOutcomesPendingAfter(ctx, tenant, 0, limit)
		}
		if err != nil {
			return count, err
		}
		for _, outcome := range pending {
			if err := ctx.Err(); err != nil {
				return count, errors.Join(failures, err)
			}
			cursor.after[tenant] = outcome.RunID
			if err := r.recordTerminal(ctx, tenant, outcome.RunID, outcome.Phase); err != nil {
				failures = errors.Join(failures, fmt.Errorf("record terminal outcome %d: %w", outcome.RunID, err))
				continue
			}
			count++
		}
	}
	return count, failures
}
