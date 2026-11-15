package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"

	"orbitjob/internal/core/app/functioninvoke"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/domain/resource"
)

// controlPlaneRevisions adapts the core store's control-plane repository to
// the function invoke use case's RevisionSource. The store answers with one
// immutable revision, including the timeout used when publishing the run,
// and a revision that is not there yet -- the operator's sync loop has not
// caught up with a new definition or version -- arrives as a NotFoundError,
// which this adapter translates into found=false, the use case's
// retryable-conflict answer.
type controlPlaneRevisions struct {
	revisions *corepostgres.ControlPlaneRepository
}

func (s controlPlaneRevisions) ActiveRevision(ctx context.Context, tenantID, sourceUID string) (functioninvoke.Revision, bool, error) {
	rev, err := s.revisions.ActiveFunctionRevision(ctx, tenantID, sourceUID)
	if err != nil {
		var notFound *resource.NotFoundError
		if errors.As(err, &notFound) {
			return functioninvoke.Revision{}, false, nil
		}
		return functioninvoke.Revision{}, false, err
	}
	var spec v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(rev.NormalizedSpec), &spec); err != nil {
		return functioninvoke.Revision{}, false, fmt.Errorf("decode function revision %d: %w", rev.ID, err)
	}
	if spec.TimeoutSeconds < 1 {
		return functioninvoke.Revision{}, false, fmt.Errorf("function revision %d has no valid timeout", rev.ID)
	}
	return functioninvoke.Revision{ID: rev.ID, DefinitionVersion: int(rev.Generation), Namespace: rev.Identity.Namespace, TimeoutSeconds: spec.TimeoutSeconds}, true, nil
}
