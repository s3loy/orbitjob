package http

import (
	"context"
	"errors"

	"orbitjob/internal/domain/resource"
)

// Historical parent ids remain usable for read-only history. Both readers are
// tenant-scoped and type-filtered; the run resolver additionally matches the
// original source UID. Triggers, edits and cancellation keep GetActive.
func workflowHistoryDefinition(ctx context.Context, definitions workflowDefinitionReader, in WorkflowGetInput) (WorkflowDefinition, error) {
	def, err := (&GetWorkflowUseCase{definitions: definitions}).get(ctx, in)
	var missing *resource.NotFoundError
	if !errors.As(err, &missing) {
		return def, err
	}
	reader, ok := definitions.(workflowRevisionReader)
	if !ok {
		return WorkflowDefinition{}, err
	}
	historical, readErr := reader.GetRevision(ctx, in.TenantID, in.ID)
	if readErr != nil {
		return WorkflowDefinition{}, readErr
	}
	if historical.ID != in.ID || historical.SourceUID == "" {
		return WorkflowDefinition{}, &resource.NotFoundError{Resource: "workflow", ID: in.ID}
	}
	return historical, nil
}
