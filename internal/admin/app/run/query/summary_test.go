package query

import (
	"context"
	"testing"
)

type summaryReader struct {
	seen  ListInput
	calls int
}

func (r *summaryReader) List(context.Context, ListInput) ([]ListItem, error) { return nil, nil }
func (r *summaryReader) Summary(_ context.Context, in ListInput) (Summary, error) {
	r.seen = in
	r.calls++
	return Summary{Total: 47, Phases: map[string]int64{"Succeeded": 46, "Running": 1}}, nil
}
func TestSummaryFullFiltersAndScopeGuard(t *testing.T) {
	repo := &summaryReader{}
	uc := NewListRunsUseCase(repo)
	in := ListInput{TenantID: "00000000000000000000000001", Phase: "Running", SourceUID: "exact", OccurrenceKey: "occ", CreatedFrom: "2026-10-19T08:00:00+08:00", CreatedBefore: "2026-10-20T00:00:00Z", Limit: 1, Offset: 45}
	out, err := uc.Summary(context.Background(), in)
	if err != nil || out.Total != 47 || repo.seen.CreatedFrom != "2026-10-19T00:00:00Z" || repo.seen.Offset != 0 || repo.seen.SourceUID != "exact" || repo.seen.OccurrenceKey != "occ" || repo.seen.Phase != "Running" {
		t.Fatalf("out=%+v input=%+v err=%v", out, repo.seen, err)
	}
	in.ResourceGroupID = "group"
	if _, err := uc.Summary(context.Background(), in); err == nil || repo.calls != 1 {
		t.Fatalf("scope guard: calls=%d err=%v", repo.calls, err)
	}
}
