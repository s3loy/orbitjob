package command

import (
	"context"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	runquery "orbitjob/internal/admin/app/run/query"
	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/platform/metrics"
)

// runLocator reads one run tenant-scoped together with the revision identity
// that names its JobRun Custom Resource. The ledger row alone does not carry
// the definition name or namespace; the store assembles the full target.
type runLocator interface {
	GetCancelTarget(ctx context.Context, in runquery.GetInput) (runquery.CancelTarget, error)
}

// runCanceller delivers a stop intent to Kubernetes. The admin API patches
// spec.cancelRequested and nothing else: the operator observes the request,
// deletes the Kubernetes Job, and owns every ledger write that follows.
type runCanceller interface {
	RequestCancel(ctx context.Context, namespace, name string) error
}

// CancelRunUseCase turns a cancel request into a spec.cancelRequested patch on
// the run's JobRun Custom Resource. Like the trigger use case it never writes
// the ledger: orbitjob_admin holds SELECT only on the control-plane tables.
type CancelRunUseCase struct {
	locator   runLocator
	canceller runCanceller
}

// NewCancelRunUseCase builds a CancelRunUseCase.
func NewCancelRunUseCase(locator runLocator, canceller runCanceller) *CancelRunUseCase {
	return &CancelRunUseCase{locator: locator, canceller: canceller}
}

// CancelInput is the admin command input for one cancel request.
type CancelInput struct {
	// RunID is the ledger id of the run to stop.
	RunID    int64
	TenantID string
	// ResourceGroupID is the scope the caller's key is limited to; empty for an
	// unscoped caller. A run has no group, so a scoped caller is refused rather
	// than allowed to cancel across the whole tenant.
	ResourceGroupID string
}

// CancelResult is what a caller gets back: a reference to the JobRun Custom
// Resource the stop intent was patched onto, and the ledger phase observed at
// request time. Phase is a snapshot, not a promise: the run reaches Canceled
// only after the operator has observed the Kubernetes Job gone, so a caller
// wanting the outcome reads the run again.
type CancelResult struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	OccurrenceKey string `json:"occurrence_key"`
	Phase         string `json:"phase"`
}

// Cancel validates the request, reads the run tenant-scoped, and patches the
// stop intent onto its JobRun. A run that already reached a terminal phase
// succeeds without patching: a finished run is final, and the current phase is
// the answer. CancelRequested and CancelUnknown runs are patched again, which
// is a no-op on the resource, so a retried request converges either way.
//
// The input carries no actor on purpose. The API writes no audit row and no
// ledger row here -- the effect travels through the Custom Resource, and the
// operator records the ledger transitions -- so there is no actor to accept,
// and refusing one in the request body keeps the field from implying it is
// recorded.
func (uc *CancelRunUseCase) Cancel(ctx context.Context, in CancelInput) (CancelResult, error) {
	normalized, err := runquery.NormalizeGetInput(runquery.GetInput{
		TenantID:        in.TenantID,
		ID:              in.RunID,
		ResourceGroupID: in.ResourceGroupID,
	})
	if err != nil {
		return CancelResult{}, err
	}
	if uc.locator == nil {
		return CancelResult{}, fmt.Errorf("run locator is required")
	}
	if uc.canceller == nil {
		return CancelResult{}, fmt.Errorf("run canceller is required")
	}

	target, err := uc.locator.GetCancelTarget(ctx, normalized)
	if err != nil {
		return CancelResult{}, fmt.Errorf("read run for cancel: %w", err)
	}

	if target.ScheduledJobName == "" || target.Namespace == "" {
		// The revision row always carries both, so an empty value means the
		// ledger row is incomplete, not that the run has no resource: refuse
		// rather than patch a name that cannot address the object.
		return CancelResult{}, fmt.Errorf("run %d does not name its definition; cancel cannot address its resource", normalized.ID)
	}

	name := v1alpha1.RunObjectName(target.ScheduledJobName, target.OccurrenceKey)
	if jobrun.Terminal(jobrun.Phase(target.Phase)) {
		return CancelResult{
			Namespace:     target.Namespace,
			Name:          name,
			OccurrenceKey: target.OccurrenceKey,
			Phase:         target.Phase,
		}, nil
	}

	if err := uc.canceller.RequestCancel(ctx, target.Namespace, name); err != nil {
		return CancelResult{}, fmt.Errorf("request cancel for run %d: %w", normalized.ID, err)
	}
	metrics.RunCancelRequestsTotal.Inc()

	return CancelResult{
		Namespace:     target.Namespace,
		Name:          name,
		OccurrenceKey: target.OccurrenceKey,
		Phase:         target.Phase,
	}, nil
}
