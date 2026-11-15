package query

import (
	"context"
	"strings"
	"testing"
)

func TestCheckSearchIsNormalizedBeforeRepository(t *testing.T) {
	repo := &stubCheckLister{}
	uc := NewListChecksUseCase(repo)
	_, err := uc.List(context.Background(), ListChecksInput{TenantID: "tenant", ResourceGroupID: "group", Search: " API_% ", Limit: 1, Offset: 2})
	if err != nil || repo.in.Search != "API_%" || repo.in.ResourceGroupID != "group" || repo.in.Offset != 2 {
		t.Fatalf("input=%+v error=%v", repo.in, err)
	}
	for _, bad := range []string{strings.Repeat("x", 129), "text\x00"} {
		if _, err := uc.List(context.Background(), ListChecksInput{Search: bad}); err == nil {
			t.Fatalf("accepted search %q", bad)
		}
		if repo.in.Search != "API_%" {
			t.Fatal("invalid search reached repository")
		}
	}
}
