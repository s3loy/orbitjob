// Package query implements authenticated workspace discovery independently of
// business tenant selectors and ordinary tenant-scoped configuration queries.
package query

import (
	"context"
	"slices"
	"unicode/utf8"

	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/queryfilter"
	"orbitjob/internal/domain/validation"
)

type Caller struct {
	TenantID        string
	ResourceGroupID string
	IsPlatform      bool
	Documents       []policy.Document
}
type ListInput struct {
	Search string
	Limit  int
	Offset int
	Caller Caller
}
type WorkspaceListItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}
type WorkspaceListResult struct {
	Items   []WorkspaceListItem `json:"items"`
	HasMore bool                `json:"has_more"`
}

// DirectoryInput is computed by the application, never decoded from a request.
// SQL applies this complete visibility set before search and pagination.
type DirectoryInput struct {
	Scope  policy.TenantScope
	Search string
	Limit  int
	Offset int
}
type directoryReader interface {
	ListWorkspaceDirectory(context.Context, DirectoryInput) ([]WorkspaceListItem, error)
}
type Lister struct{ reader directoryReader }

func NewLister(reader directoryReader) *Lister { return &Lister{reader: reader} }

func (l *Lister) List(ctx context.Context, in ListInput) (WorkspaceListResult, error) {
	if !utf8.ValidString(in.Search) {
		return WorkspaceListResult{}, validation.New("search", "must be valid UTF-8")
	}
	search, err := queryfilter.Search(in.Search)
	if err != nil {
		return WorkspaceListResult{}, err
	}
	in.Search = search
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 100 {
		return WorkspaceListResult{}, validation.New("limit", "must be between 1 and 100")
	}
	if in.Offset < 0 || int64(in.Offset) > 2147483647 {
		return WorkspaceListResult{}, validation.New("offset", "must be between 0 and 2147483647")
	}
	if !in.Caller.IsPlatform && in.Caller.TenantID == "" {
		return WorkspaceListResult{}, validation.New("caller", "authenticated tenant identity is required")
	}
	scope := policy.TenantReadScope(in.Caller.Documents)
	if in.Caller.ResourceGroupID != "" {
		scope = policy.TenantScope{}
	}
	// A tenant's own display metadata is implicit identity information (ADR0019),
	// even without tenant:Get. This exception never applies to foreign tenants.
	if !in.Caller.IsPlatform && in.Caller.TenantID != "" {
		if scope.All {
			scope.Exclude = slices.DeleteFunc(scope.Exclude, func(id string) bool { return id == in.Caller.TenantID })
		} else if !slices.Contains(scope.Include, in.Caller.TenantID) {
			scope.Include = append(scope.Include, in.Caller.TenantID)
		}
	}
	if !scope.All && len(scope.Include) == 0 {
		return WorkspaceListResult{Items: []WorkspaceListItem{}}, nil
	}
	rows, err := l.reader.ListWorkspaceDirectory(ctx, DirectoryInput{Scope: scope, Search: in.Search, Limit: in.Limit + 1, Offset: in.Offset})
	if err != nil {
		return WorkspaceListResult{}, err
	}
	out := WorkspaceListResult{Items: rows, HasMore: len(rows) > in.Limit}
	if out.HasMore {
		out.Items = rows[:in.Limit]
	}
	if out.Items == nil {
		out.Items = []WorkspaceListItem{}
	}
	return out, nil
}
