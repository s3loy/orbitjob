package query

import (
	"strings"
	"testing"
)

func TestNormalizeJobExactFilters(t *testing.T) {
	got, err := NormalizeListInput(ListInput{TenantID: ulidTenant, Namespace: " prod ", SourceUID: " uid-1 "})
	if err != nil || got.Namespace != "prod" || got.SourceUID != "uid-1" {
		t.Fatalf("input=%+v error=%v", got, err)
	}
	for _, in := range []ListInput{
		{TenantID: ulidTenant, Namespace: strings.Repeat("a", 64)},
		{TenantID: ulidTenant, Namespace: "prod\x00"},
		{TenantID: ulidTenant, SourceUID: strings.Repeat("a", 129)},
		{TenantID: ulidTenant, SourceUID: "uid\x00"},
	} {
		if _, err := NormalizeListInput(in); err == nil {
			t.Fatalf("accepted invalid filter: %+v", in)
		}
	}
}
