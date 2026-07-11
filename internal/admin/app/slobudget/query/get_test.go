package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
)

type stubBudgetReader struct {
	out slo.Budget
	err error
}

func (s *stubBudgetReader) GetCurrent(_ context.Context, _ string, _ int64) (slo.Budget, error) {
	return s.out, s.err
}

type stubBudgetLister struct {
	lastLimit int
	items     []slo.Budget
	total     int64
	err       error
}

func (s *stubBudgetLister) ListHistory(_ context.Context, _ string, _ int64, limit, _ int) ([]slo.Budget, int64, error) {
	s.lastLimit = limit
	return s.items, s.total, s.err
}

func TestGetBudget_Success(t *testing.T) {
	now := time.Now()
	repo := &stubBudgetReader{out: slo.Budget{
		WindowStart: now, WindowEnd: now.Add(time.Hour),
		BudgetTotal: 100, BudgetConsumed: 10, BudgetRemaining: 90, BurnRate: 0.5, Status: "ok", Version: 1,
	}}
	uc := NewGetBudgetUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BudgetTotal != 100 || result.Status != "ok" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetBudget_InvalidSLOID(t *testing.T) {
	repo := &stubBudgetReader{}
	uc := NewGetBudgetUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 0)
	if err == nil {
		t.Fatal("expected validation error for slo_id<=0")
	}
}

func TestGetBudget_NoDataYet(t *testing.T) {
	repo := &stubBudgetReader{err: &resource.NotFoundError{Resource: "budget", ID: 5}}
	uc := NewGetBudgetUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 5)
	if err != nil {
		t.Fatalf("expected no error for not-found budget (should return empty), got %v", err)
	}
	if result.Status != "no_data" {
		t.Errorf("expected status 'no_data', got %q", result.Status)
	}
	if result.BudgetTotal != 0 || result.BudgetConsumed != 0 || result.BudgetRemaining != 0 {
		t.Errorf("expected zero budgets, got total=%f consumed=%f remaining=%f",
			result.BudgetTotal, result.BudgetConsumed, result.BudgetRemaining)
	}
}

func TestGetBudget_Error(t *testing.T) {
	repo := &stubBudgetReader{err: errors.New("not found")}
	uc := NewGetBudgetUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListBudgetHistory_Success(t *testing.T) {
	now := time.Now()
	repo := &stubBudgetLister{
		items: []slo.Budget{{WindowStart: now, WindowEnd: now.Add(time.Hour), BudgetTotal: 100}},
		total: 1,
	}
	uc := NewListBudgetHistoryUseCase(repo)

	result, err := uc.List(context.Background(), ListInput{TenantID: "t1", SLOID: 5, Limit: 10})
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

func TestListBudgetHistory_Error(t *testing.T) {
	repo := &stubBudgetLister{err: errors.New("db down")}
	uc := NewListBudgetHistoryUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: "t1", SLOID: 5})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListBudgetHistory_DefaultLimit(t *testing.T) {
	repo := &stubBudgetLister{}
	uc := NewListBudgetHistoryUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", SLOID: 5, Limit: 0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 50 {
		t.Fatalf("expected default limit 50, got %d", repo.lastLimit)
	}
}

func TestListBudgetHistory_ClampsLimit(t *testing.T) {
	repo := &stubBudgetLister{}
	uc := NewListBudgetHistoryUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", SLOID: 5, Limit: 500}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 100 {
		t.Fatalf("expected limit clamped to 100, got %d", repo.lastLimit)
	}
}
