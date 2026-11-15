package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/resourcegroup"

	"orbitjob/internal/platform/scan"
)

type stubGroupLister struct {
	groups      []resourcegroup.Group
	err         error
	gotTenantID string
}

func (s *stubGroupLister) List(_ context.Context, tenantID string) ([]resourcegroup.Group, error) {
	s.gotTenantID = tenantID
	return s.groups, s.err
}

func TestListMapsEveryGroup(t *testing.T) {
	repo := &stubGroupLister{groups: []resourcegroup.Group{
		{ID: "g1", TenantID: resourcegroupListFixtureTenantID, Slug: "ci", Name: "Continuous Integration"},
		{ID: "g2", TenantID: resourcegroupListFixtureTenantID, Slug: "prod", Name: "Production"},
	}}

	items, err := NewLister(repo).List(context.Background(), ListInput{TenantID: resourcegroupListFixtureTenantID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.gotTenantID != resourcegroupListFixtureTenantID {
		t.Fatalf("expected the caller's tenant, got %q", repo.gotTenantID)
	}
	if len(items) != 2 || items[0].Slug != "ci" || items[1].ID != "g2" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestListReturnsAnEmptySliceRatherThanNil(t *testing.T) {
	items, err := NewLister(&stubGroupLister{}).List(context.Background(), ListInput{TenantID: resourcegroupListFixtureTenantID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if items == nil {
		t.Fatal("expected an empty slice; nil marshals to JSON null, not []")
	}
}

func TestListPropagatesAStoreError(t *testing.T) {
	repo := &stubGroupLister{err: errors.New("db down")}
	if _, err := NewLister(repo).List(context.Background(), ListInput{TenantID: resourcegroupListFixtureTenantID}); err == nil {
		t.Fatal("expected the store error to propagate")
	}
}

var resourcegroupListFixtureTenantID = scan.GenerateID()
