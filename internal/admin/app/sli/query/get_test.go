package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/sli"
)

type stubSLIReader struct {
	out sli.Snapshot
	err error
}

func (s *stubSLIReader) Get(_ context.Context, _ string, _ int64) (sli.Snapshot, error) {
	return s.out, s.err
}

type stubSLILister struct {
	lastLimit int
	items     []sli.Snapshot
	total     int64
	err       error
}

func (s *stubSLILister) List(_ context.Context, _ string, limit, _ int) ([]sli.Snapshot, int64, error) {
	s.lastLimit = limit
	return s.items, s.total, s.err
}

func TestGetSLI_Success(t *testing.T) {
	now := time.Now()
	repo := &stubSLIReader{out: sli.Snapshot{
		ID: 1, Name: "sli-1", SLIType: sli.TypeAvailability, CreatedAt: now, UpdatedAt: now,
	}}
	uc := NewGetSLIUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.Name != "sli-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetSLI_Error(t *testing.T) {
	repo := &stubSLIReader{err: errors.New("not found")}
	uc := NewGetSLIUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListSLIs_Success(t *testing.T) {
	repo := &stubSLILister{
		items: []sli.Snapshot{{ID: 1, Name: "a", CreatedAt: time.Now()}},
		total: 1,
	}
	uc := NewListSLIsUseCase(repo)

	result, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.lastLimit != 10 {
		t.Fatalf("expected limit 10 forwarded, got %d", repo.lastLimit)
	}
}

func TestListSLIs_Error(t *testing.T) {
	repo := &stubSLILister{err: errors.New("db down")}
	uc := NewListSLIsUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListSLIs_DefaultLimitWhenZeroOrNegative(t *testing.T) {
	repo := &stubSLILister{}
	uc := NewListSLIsUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 50 {
		t.Fatalf("expected default limit 50 when limit<=0, got %d", repo.lastLimit)
	}
}

func TestListSLIs_ClampsLimitToMax(t *testing.T) {
	repo := &stubSLILister{}
	uc := NewListSLIsUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 200}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 100 {
		t.Fatalf("expected limit clamped to 100, got %d", repo.lastLimit)
	}
}
