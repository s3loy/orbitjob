package operator

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// This lane shares the execution recovery timer but uses the attempt row ID:
// an old attempt's held Pods must remain reachable after a run becomes terminal.
func (r Runtime) recoverPodEvidence(ctx context.Context, tenant string, limit int, cursor *ExecutionRecoveryCursor, store retainedEvidenceStore) (int, error) {
	rows, err := store.PendingEvidenceAfter(ctx, tenant, cursor.EvidenceAfter[tenant], limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		cursor.EvidenceAfter[tenant] = 0
		return 0, nil
	}
	settled := 0
	var failures []error
	for _, a := range rows {
		cursor.EvidenceAfter[tenant] = a.ID
		itemCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		job, _, getErr := r.Jobs.Get(itemCtx, a.Execution.Namespace, a.JobName)
		if getErr == nil {
			var done bool
			done, getErr = r.settlePodEvidence(itemCtx, tenant, a, job)
			if done {
				settled++
			}
		}
		cancel()
		if getErr != nil {
			executionObservations.WithLabelValues("hold_recovery_error").Inc()
			slog.ErrorContext(ctx, "Pod evidence recovery failed", "tenant", tenant, "run_id", a.Execution.Run.ID, "attempt", a.Number, "error", getErr)
			failures = append(failures, getErr)
		}
	}
	return settled, errors.Join(failures...)
}
