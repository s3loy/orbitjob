package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/tenant"
)

type stubTenantListReader struct {
	called     bool
	limit      int
	offset     int
	returnList []tenant.Tenant
	err        error
}

func (s *stubTenantListReader) List(ctx context.Context, limit, offset int) ([]tenant.Tenant, error) {
	s.called = true
	s.limit = limit
	s.offset = offset
	return s.returnList, s.err
}

func TestLister_List_Defaults(t *testing.T) {
	repo := &stubTenantListReader{returnList: []tenant.Tenant{{ID: "t1", Slug: "acme"}}}
	uc := NewLister(repo)

	out, err := uc.List(context.Background(), ListInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.called {
		t.Fatal("expected repo.List to be called")
	}
	if repo.limit != defaultListLimit {
		t.Fatalf("expected default limit=%d, got %d", defaultListLimit, repo.limit)
	}
	if repo.offset != 0 {
		t.Fatalf("expected offset=0, got %d", repo.offset)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
}

func TestLister_List_CapsLimit(t *testing.T) {
	repo := &stubTenantListReader{}
	uc := NewLister(repo)

	_, err := uc.List(context.Background(), ListInput{Limit: 500, Offset: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.limit != maxListLimit {
		t.Fatalf("expected capped limit=%d, got %d", maxListLimit, repo.limit)
	}
	if repo.offset != 10 {
		t.Fatalf("expected offset=10, got %d", repo.offset)
	}
}

func TestLister_List_NegativeOffset(t *testing.T) {
	repo := &stubTenantListReader{}
	uc := NewLister(repo)

	_, err := uc.List(context.Background(), ListInput{Offset: -5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.offset != 0 {
		t.Fatalf("expected offset=0, got %d", repo.offset)
	}
}

func TestLister_List_RepoError(t *testing.T) {
	repo := &stubTenantListReader{err: errors.New("db down")}
	uc := NewLister(repo)

	_, err := uc.List(context.Background(), ListInput{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
