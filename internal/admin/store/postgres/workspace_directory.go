package postgres

import (
	"context"
	"fmt"

	"github.com/lib/pq"
	query "orbitjob/internal/admin/app/workspace/query"
)

// ListWorkspaceDirectory uses the dedicated, metadata-only definer. Its scope
// arguments come from effective authorization, not from user query selectors.
// Ordinary TenantRepository.List/Get and their tenant GUC remain unchanged.
func (r *TenantRepository) ListWorkspaceDirectory(ctx context.Context, in query.DirectoryInput) ([]query.WorkspaceListItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,slug,status FROM orbitjob_workspace_directory($1,$2::text[],$3::text[],$4,$5,$6)`, in.Scope.All, pq.Array(in.Scope.Include), pq.Array(in.Scope.Exclude), in.Search, in.Limit, in.Offset)
	if err != nil {
		return nil, fmt.Errorf("query workspace directory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []query.WorkspaceListItem{}
	for rows.Next() {
		var item query.WorkspaceListItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Status); err != nil {
			return nil, fmt.Errorf("scan workspace directory: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read workspace directory: %w", err)
	}
	return out, nil
}
