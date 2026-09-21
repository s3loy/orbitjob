package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/policy"
)

type stubPolicyLister struct {
	records []policy.Record
	err     error
}

func (s *stubPolicyLister) List(_ context.Context, _ string) ([]policy.Record, error) {
	return s.records, s.err
}

func TestListMarksPlatformPresets(t *testing.T) {
	repo := &stubPolicyLister{records: []policy.Record{
		{ID: "preset", Name: "ReadOnlyAccess"},           // TenantID empty: a preset
		{ID: "own", Name: "JobOperator", TenantID: "t1"}, // the tenant's own
	}}

	items, err := NewLister(repo).List(context.Background(), ListInput{TenantID: "t1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// The flag is what tells a caller which entries it may delete, so it has to
	// be right in both directions.
	if !items[0].Platform {
		t.Error("a preset was not marked as platform-owned")
	}
	if items[1].Platform {
		t.Error("a tenant-owned policy was marked as a platform preset")
	}
}

func TestListReturnsTheDocument(t *testing.T) {
	doc := policy.Document{Version: "1", Statement: []policy.Statement{{
		Effect:   policy.EffectAllow,
		Action:   []string{"job:Create"},
		Resource: []string{"orbitjob:self:*:job/*"},
	}}}
	repo := &stubPolicyLister{records: []policy.Record{{ID: "p1", Document: doc}}}

	items, err := NewLister(repo).List(context.Background(), ListInput{TenantID: "t1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Without the document a caller cannot tell what it is about to bind.
	if len(items[0].Document.Statement) != 1 {
		t.Fatalf("the document is missing from the list item: %+v", items[0])
	}
}

func TestListReturnsAnEmptySliceRatherThanNil(t *testing.T) {
	items, err := NewLister(&stubPolicyLister{}).List(context.Background(), ListInput{TenantID: "t1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if items == nil {
		t.Fatal("expected an empty slice; nil marshals to JSON null, not []")
	}
	if len(items) != 0 {
		t.Fatalf("expected no items, got %d", len(items))
	}
}

func TestListPropagatesAStoreError(t *testing.T) {
	repo := &stubPolicyLister{err: errors.New("db down")}
	if _, err := NewLister(repo).List(context.Background(), ListInput{TenantID: "t1"}); err == nil {
		t.Fatal("expected the store error to propagate")
	}
}
