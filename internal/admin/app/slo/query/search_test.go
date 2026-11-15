package query

import (
	"context"
	"orbitjob/internal/core/domain/slo"
	"strings"
	"testing"
)

type searchableSLOStub struct {
	stubSLOLister
	search, tenant, group string
	calls                 int
}

func (r *searchableSLOStub) ListSearch(_ context.Context, tenant, group, search string, limit, offset int) ([]slo.Snapshot, int64, error) {
	r.search, r.tenant, r.group, r.calls = search, tenant, group, r.calls+1
	return r.items, r.total, r.err
}

func TestSLOSearchIsNormalizedAndCannotSilentlyFilterOnePage(t *testing.T) {
	repo := &searchableSLOStub{}
	uc := NewListSLOsUseCase(repo)
	_, err := uc.List(context.Background(), ListInput{TenantID: "tenant", ResourceGroupID: "group", Search: " API_% ", Limit: 1, Offset: 2})
	if err != nil || repo.search != "API_%" || repo.tenant != "tenant" || repo.group != "group" {
		t.Fatalf("reader=%+v error=%v", repo, err)
	}
	for _, bad := range []string{strings.Repeat("x", 129), "text\x00"} {
		if _, err := uc.List(context.Background(), ListInput{Search: bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if repo.calls != 1 {
		t.Fatal("invalid search reached repository")
	}
	if _, err := NewListSLOsUseCase(&stubSLOLister{}).List(context.Background(), ListInput{Search: "API"}); err == nil {
		t.Fatal("an adapter without search must not silently serve its first page")
	}
}
