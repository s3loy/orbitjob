package main

import (
	"context"

	adminhttp "orbitjob/internal/admin/http"
	corepostgres "orbitjob/internal/core/store/postgres"
)

// workflowDefinitionSource adapts the core store's workflow-definition
// projection to the admin HTTP surface's reader. It is wiring, not logic: the
// store returns neutral projection rows (the spec already decodes to the same
// v1alpha1 shape the walker and the API share), and the two packages are
// pinned together here so neither has to import the other.
type workflowDefinitionSource struct {
	definitions *corepostgres.WorkflowDefinitionRepository
}

func (s workflowDefinitionSource) ListActive(ctx context.Context, in adminhttp.WorkflowDefinitionListInput) ([]adminhttp.WorkflowDefinition, error) {
	rows, err := s.definitions.ListActiveWorkflowDefinitions(ctx, in.TenantID, in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	out := make([]adminhttp.WorkflowDefinition, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminhttp.WorkflowDefinition{
			ID:         row.ID,
			Name:       row.Name,
			Namespace:  row.Namespace,
			SourceUID:  row.SourceUID,
			Generation: row.Generation,
			Spec:       row.Spec,
			CreatedAt:  row.CreatedAt,
		})
	}
	return out, nil
}

func (s workflowDefinitionSource) GetActive(ctx context.Context, tenantID string, id int64) (adminhttp.WorkflowDefinition, error) {
	row, err := s.definitions.ActiveWorkflowDefinition(ctx, tenantID, id)
	if err != nil {
		return adminhttp.WorkflowDefinition{}, err
	}
	return adminhttp.WorkflowDefinition{
		ID:         row.ID,
		Name:       row.Name,
		Namespace:  row.Namespace,
		SourceUID:  row.SourceUID,
		Generation: row.Generation,
		Spec:       row.Spec,
		CreatedAt:  row.CreatedAt,
	}, nil
}
