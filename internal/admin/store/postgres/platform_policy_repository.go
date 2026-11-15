package postgres

import (
	"context"
	"fmt"

	"orbitjob/internal/core/domain/policy"
)

// EffectivePlatformDocuments loads only the authenticated platform credential's
// installation presets. Do not resolve self against a caller-selected target:
// a platform principal has no tenant, so self grants cannot acquire one here.
func (r *PolicyRepository) EffectivePlatformDocuments(ctx context.Context, keyID string) ([]policy.Document, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT policy_id, document, is_boundary FROM orbitjob_platform_key_documents($1)`, keyID)
	if err != nil {
		return nil, fmt.Errorf("load platform policies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var documents []policy.Document
	var boundary *policy.Document
	for rows.Next() {
		var id string
		var raw []byte
		var isBoundary bool
		if err := rows.Scan(&id, &raw, &isBoundary); err != nil {
			return nil, fmt.Errorf("scan platform policy: %w", err)
		}
		doc, err := policy.ParseDocument(raw)
		if err != nil {
			return nil, fmt.Errorf("parse platform policy %s: %w", id, err)
		}
		if isBoundary {
			boundary = &doc
		} else {
			documents = append(documents, doc)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform policies: %w", err)
	}
	if boundary != nil {
		return policy.Intersect(documents, *boundary), nil
	}
	return documents, nil
}
