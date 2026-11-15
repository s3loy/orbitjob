package query

import (
	"context"
	"fmt"
)

// Summary is authoritative for the entire filtered ledger, not one page.
// Phases contains only phases actually present in the matching rows.
type Summary struct {
	Total  int64            `json:"total"`
	Phases map[string]int64 `json:"phases"`
}

type runSummarizer interface {
	Summary(context.Context, ListInput) (Summary, error)
}

// Summary shares list normalization and therefore the same scope and filters.
func (uc *ListRunsUseCase) Summary(ctx context.Context, in ListInput) (Summary, error) {
	in.Limit = 0
	in.Offset = 0
	normalized, err := NormalizeListInput(in)
	if err != nil {
		return Summary{}, err
	}
	repo, ok := uc.repo.(runSummarizer)
	if !ok {
		return Summary{}, fmt.Errorf("run summary reader is required")
	}
	return repo.Summary(ctx, normalized)
}
