package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/slo"
)

type stubSLOReader struct {
	out slo.Snapshot
	err error
}

func (s *stubSLOReader) Get(_ context.Context, _ string, _ int64) (slo.Snapshot, error) {
	return s.out, s.err
}

type stubBudgetReader struct {
	out slo.Budget
	err error
}

func (s *stubBudgetReader) GetCurrent(_ context.Context, _ string, _ int64) (slo.Budget, error) {
	return s.out, s.err
}

type stubSLOLister struct {
	lastLimit int
	items     []slo.Snapshot
	total     int64
	err       error
}

func (s *stubSLOLister) List(_ context.Context, _ string, limit, _ int) ([]slo.Snapshot, int64, error) {
	s.lastLimit = limit
	return s.items, s.total, s.err
}

func TestGetSLO_WithBudget(t *testing.T) {
	now := time.Now()
	sloRepo := &stubSLOReader{out: slo.Snapshot{
		ID: 1, Name: "slo-1", WindowDuration: time.Hour, CreatedAt: now, UpdatedAt: now,
	}}
	budgetRepo := &stubBudgetReader{out: slo.Budget{
		WindowStart: now, WindowEnd: now.Add(time.Hour),
		BudgetTotal: 100, BudgetConsumed: 10, BudgetRemaining: 90, BurnRate: 0.5, Status: "ok",
	}}
	uc := NewGetSLOUseCase(sloRepo, budgetRepo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.Name != "slo-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.CurrentBudget == nil {
		t.Fatal("expected current budget to be populated")
	}
	if result.CurrentBudget.BudgetTotal != 100 {
		t.Fatalf("expected budget total 100, got %v", result.CurrentBudget.BudgetTotal)
	}
}

func TestGetSLO_NoBudgetOnBudgetError(t *testing.T) {
	// Budget lookup failure must not fail Get; CurrentBudget stays nil.
	sloRepo := &stubSLOReader{out: slo.Snapshot{
		ID: 1, Name: "slo-1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	budgetRepo := &stubBudgetReader{err: errors.New("no budget yet")}
	uc := NewGetSLOUseCase(sloRepo, budgetRepo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CurrentBudget != nil {
		t.Fatal("expected nil budget when budget lookup errors")
	}
}

func TestGetSLO_SLOError(t *testing.T) {
	sloRepo := &stubSLOReader{err: errors.New("not found")}
	uc := NewGetSLOUseCase(sloRepo, &stubBudgetReader{})

	_, err := uc.Get(context.Background(), "t1", 1)
	if err == nil {
		t.Fatal("expected error from SLO repo")
	}
}

func TestListSLOs_Success(t *testing.T) {
	repo := &stubSLOLister{
		items: []slo.Snapshot{{ID: 1, Name: "a", CreatedAt: time.Now()}},
		total: 1,
	}
	uc := NewListSLOsUseCase(repo)

	result, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.lastLimit != 10 {
		t.Fatalf("expected limit 10, got %d", repo.lastLimit)
	}
}

func TestListSLOs_Error(t *testing.T) {
	repo := &stubSLOLister{err: errors.New("db down")}
	uc := NewListSLOsUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListSLOs_DefaultLimitWhenZeroOrNegative(t *testing.T) {
	repo := &stubSLOLister{}
	uc := NewListSLOsUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 50 {
		t.Fatalf("expected default limit 50, got %d", repo.lastLimit)
	}
}

func TestListSLOs_ClampsLimitToMax(t *testing.T) {
	repo := &stubSLOLister{}
	uc := NewListSLOsUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 500}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 100 {
		t.Fatalf("expected limit clamped to 100, got %d", repo.lastLimit)
	}
}
