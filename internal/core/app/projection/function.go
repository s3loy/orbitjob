package projection

import (
	"context"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/domain/function"
	"orbitjob/internal/core/domain/revision"
)

// FunctionJobSpec synthesizes the ScheduledJobSpec a function revision pins.
// The mapping (the checks convention): the definition's image, command and
// args travel verbatim, timeout_seconds becomes the run deadline the operator
// projects onto the Kubernetes Job, and retry_limit (retries) becomes the
// platform attempt budget (RetryLimit+1 attempts).
//
// Scheduling is deliberately absent: a function is invoke-only, and the
// synthesized spec has an empty Schedule so the job scheduler can never fire
// this revision on its own.
func FunctionJobSpec(def function.Definition) v1alpha1.ScheduledJobSpec {
	timeout := def.TimeoutSeconds
	if timeout < 1 {
		timeout = function.DefaultTimeoutSeconds
	}
	return v1alpha1.ScheduledJobSpec{
		TimeoutSeconds: int32(timeout),
		RetryPolicy:    v1alpha1.RetryPolicy{MaxAttempts: int32(def.RetryLimit + 1)},
		JobTemplate: v1alpha1.JobTemplateSpec{
			Image:   def.Image,
			Command: def.Command,
			Args:    def.Args,
		},
	}
}

// ApplyFunction materializes a function version as an immutable definition
// revision, the same store path a ScheduledJob CR projects through. The
// revision is keyed source_mode=function, source_uid=function-<id>,
// generation=functions.version, so re-projecting a version is idempotent and
// saving the definition makes a new revision while in-flight invocations keep
// the one they were pinned to.
//
// The revision inherits the function row's resource group so the ledger
// carries the scoping the definition was created under, exactly as a
// check-sourced revision does.
func (s Service) ApplyFunction(ctx context.Context, def function.Definition, namespace, tenantID, actor string) (int64, error) {
	if tenantID == "" {
		return 0, fmt.Errorf("tenant is required")
	}
	if namespace == "" {
		return 0, fmt.Errorf("namespace is required")
	}
	if s.Revisions == nil {
		return 0, fmt.Errorf("revision writer is required")
	}
	if def.ID < 1 {
		return 0, fmt.Errorf("function id is required")
	}
	sourceUID := function.SourceUID(def.ID)
	rev, err := revision.New(
		revision.Identity{
			SourceMode: function.SourceModeFunction,
			SourceUID:  sourceUID,
			Namespace:  namespace,
			Name:       sourceUID,
		},
		int64(def.Version),
		normalizeSpec(FunctionJobSpec(def)),
		actor,
		def.ResourceGroupID,
		// A revision is created now, not when the definition was: re-projecting
		// an old row must not backdate the revision it produces.
		s.now(),
	)
	if err != nil {
		return 0, err
	}
	return s.Revisions.ApplyRevisionForTenant(ctx, tenantID, rev)
}
