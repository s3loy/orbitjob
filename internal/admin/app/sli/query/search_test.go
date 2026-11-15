package query

import (
	"context"
	"orbitjob/internal/core/domain/sli"
	"strings"
	"testing"
)

type searchableSLIStub struct {
	stubSLILister
	search, tenant, group string
	calls                 int
}

func (r *searchableSLIStub) ListSearch(_ context.Context, tenant, group, search string, limit, offset int) ([]sli.Snapshot, int64, error) {
	r.search, r.tenant, r.group, r.calls = search, tenant, group, r.calls+1
	return r.items, r.total, r.err
}

func TestSLISearchIsNormalizedAndCannotSilentlyFilterOnePage(t *testing.T) {
	repo := &searchableSLIStub{}
	uc := NewListSLIsUseCase(repo)
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
	if _, err := NewListSLIsUseCase(&stubSLILister{}).List(context.Background(), ListInput{Search: "API"}); err == nil {
		t.Fatal("an adapter without search must not silently serve its first page")
	}
}
