package operator

import (
	"context"
	"encoding/json"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/domain/executionpolicy"
)

func (r Runtime) suppressOOMRetry(ctx context.Context, tenant string, stored jobrun.StoredRun) (bool, error) {
	if stored.Phase != jobrun.RetryWaiting {
		return false, nil
	}
	store, ok := r.Runs.(executionRecoveryStore)
	if !ok {
		return false, nil
	}
	e, found, err := store.CurrentExecution(ctx, tenant, stored.ID)
	if err != nil || !found {
		return false, err
	}
	if e.Run.Attempt != stored.Attempt || e.Run.Phase != jobrun.RetryWaiting || !jobrun.HasOOMTerminationEvidence(e.Run.TerminationEvidence, e.Run.AttemptUID) {
		return false, nil
	}
	rev, err := r.Revisions.RevisionByID(ctx, tenant, stored.RevisionID)
	if err != nil {
		return false, err
	}
	var spec v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &spec); err != nil {
		return false, fmt.Errorf("decode pinned retry policy: %w", err)
	}
	return spec.ExecutionPolicyVersion == executionpolicy.Version1, nil
}
