package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubCheckRunGetter struct {
	out GetResult
	err error
}

func (s *stubCheckRunGetter) Get(_ context.Context, _ string, _ int64) (GetResult, error) {
	return s.out, s.err
}

type stubCheckRunLister struct {
	in    ListCheckRunsInput
	items []ListItem
	total int
	err   error
}

func (s *stubCheckRunLister) List(_ context.Context, in ListCheckRunsInput) ([]ListItem, int, error) {
	s.in = in
	return s.items, s.total, s.err
}

func TestGetCheckRun_Success(t *testing.T) {
	now := time.Now()
	repo := &stubCheckRunGetter{out: GetResult{
		ID: 1, RunID: "r1", TenantID: "t1", CheckID: 5, Status: "success", ScheduledAt: now, CreatedAt: now,
	}}
	uc := NewGetCheckRunUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.RunID != "r1" || result.CheckID != 5 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetCheckRun_Error(t *testing.T) {
	repo := &stubCheckRunGetter{err: errors.New("not found")}
	uc := NewGetCheckRunUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListCheckRuns_Success(t *testing.T) {
	repo := &stubCheckRunLister{items: []ListItem{{ID: 1, RunID: "r1"}}, total: 1}
	uc := NewListCheckRunsUseCase(repo)

	result, err := uc.List(context.Background(), ListCheckRunsInput{TenantID: "t1", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.in.TenantID != "t1" {
		t.Fatalf("expected tenant t1, got %s", repo.in.TenantID)
	}
}

func TestListCheckRuns_Error(t *testing.T) {
	repo := &stubCheckRunLister{err: errors.New("db down")}
	uc := NewListCheckRunsUseCase(repo)

	_, err := uc.List(context.Background(), ListCheckRunsInput{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected error")
	}
}
