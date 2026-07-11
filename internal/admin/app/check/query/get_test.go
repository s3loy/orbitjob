package query

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincheck "orbitjob/internal/core/domain/check"
)

type stubCheckGetter struct {
	out domaincheck.Snapshot
	err error
}

func (s *stubCheckGetter) Get(_ context.Context, _ string, _ int64) (domaincheck.Snapshot, error) {
	return s.out, s.err
}

type stubCheckLister struct {
	in    ListChecksInput
	items []ListItem
	total int
	err   error
}

func (s *stubCheckLister) List(_ context.Context, in ListChecksInput) ([]ListItem, int, error) {
	s.in = in
	return s.items, s.total, s.err
}

func TestGetCheck_Success(t *testing.T) {
	now := time.Now()
	repo := &stubCheckGetter{out: domaincheck.Snapshot{
		ID: 1, Name: "check-1", TenantID: "t1", Status: "active",
		CheckType: domaincheck.CheckTypeHTTPHealth, CreatedAt: now, UpdatedAt: now,
	}}
	uc := NewGetCheckUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.Name != "check-1" || result.TenantID != "t1" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetCheck_Error(t *testing.T) {
	repo := &stubCheckGetter{err: errors.New("not found")}
	uc := NewGetCheckUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListChecks_Success(t *testing.T) {
	repo := &stubCheckLister{items: []ListItem{{ID: 1, Name: "a"}}, total: 1}
	uc := NewListChecksUseCase(repo)

	result, err := uc.List(context.Background(), ListChecksInput{TenantID: "t1", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.in.TenantID != "t1" || repo.in.Limit != 10 {
		t.Fatalf("expected input t1/10, got %+v", repo.in)
	}
}

func TestListChecks_Error(t *testing.T) {
	repo := &stubCheckLister{err: errors.New("db down")}
	uc := NewListChecksUseCase(repo)

	_, err := uc.List(context.Background(), ListChecksInput{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected error")
	}
}
