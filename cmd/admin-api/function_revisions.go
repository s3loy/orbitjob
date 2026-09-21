package main

import (
	"context"
	"errors"

	"orbitjob/internal/core/app/functioninvoke"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/domain/resource"
)

// controlPlaneRevisions adapts the core store's control-plane repository to
// the function invoke use case's RevisionSource. It is wiring, not logic: the
// store answers with the active revision's id and namespace as plain values,
// and a revision that is not there yet -- the operator's sync loop has not
// caught up with a new definition or version -- arrives as a NotFoundError,
// which this adapter translates into found=false, the use case's
// retryable-conflict answer.
type controlPlaneRevisions struct {
	revisions *corepostgres.ControlPlaneRepository
}

func (s controlPlaneRevisions) ActiveRevision(ctx context.Context, tenantID, sourceUID string) (functioninvoke.Revision, bool, error) {
	id, namespace, err := s.revisions.ActiveFunctionRevision(ctx, tenantID, sourceUID)
	if err != nil {
		var notFound *resource.NotFoundError
		if errors.As(err, &notFound) {
			return functioninvoke.Revision{}, false, nil
		}
		return functioninvoke.Revision{}, false, err
	}
	return functioninvoke.Revision{ID: id, Namespace: namespace}, true, nil
}
