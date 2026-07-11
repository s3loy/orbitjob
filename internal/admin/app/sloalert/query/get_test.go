package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubAlertReader struct {
	out BudgetAlertItem
	err error
}

func (s *stubAlertReader) Get(_ context.Context, _ string, _ int64) (BudgetAlertItem, error) {
	return s.out, s.err
}

type stubAlertLister struct {
	lastLimit int
	items     []BudgetAlertItem
	total     int64
	err       error
}

func (s *stubAlertLister) List(_ context.Context, _ string, _ *int64, _ *string, limit, _ int) ([]BudgetAlertItem, int64, error) {
	s.lastLimit = limit
	return s.items, s.total, s.err
}

func TestGetAlert_Success(t *testing.T) {
	now := time.Now()
	repo := &stubAlertReader{out: BudgetAlertItem{
		ID: 1, SLOID: 5, BudgetID: 2, AlertType: "fast_burn", BurnRate: 0.9, Status: "firing", TriggeredAt: now,
	}}
	uc := NewGetAlertUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.AlertType != "fast_burn" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ResolvedAt != nil {
		t.Fatal("expected nil ResolvedAt for unresolved alert")
	}
}

func TestGetAlert_WithResolved(t *testing.T) {
	now := time.Now()
	resolved := now.Add(time.Minute)
	repo := &stubAlertReader{out: BudgetAlertItem{
		ID: 1, AlertType: "fast_burn", Status: "resolved", TriggeredAt: now, ResolvedAt: &resolved,
	}}
	uc := NewGetAlertUseCase(repo)

	result, err := uc.Get(context.Background(), "t1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ResolvedAt == nil {
		t.Fatal("expected ResolvedAt populated for resolved alert")
	}
}

func TestGetAlert_InvalidID(t *testing.T) {
	repo := &stubAlertReader{}
	uc := NewGetAlertUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 0)
	if err == nil {
		t.Fatal("expected validation error for id<=0")
	}
}

func TestGetAlert_Error(t *testing.T) {
	repo := &stubAlertReader{err: errors.New("not found")}
	uc := NewGetAlertUseCase(repo)

	_, err := uc.Get(context.Background(), "t1", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListAlerts_Success(t *testing.T) {
	now := time.Now()
	repo := &stubAlertLister{
		items: []BudgetAlertItem{{ID: 1, AlertType: "fast_burn", TriggeredAt: now}},
		total: 1,
	}
	uc := NewListAlertsUseCase(repo)

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

func TestListAlerts_Error(t *testing.T) {
	repo := &stubAlertLister{err: errors.New("db down")}
	uc := NewListAlertsUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListAlerts_DefaultLimit(t *testing.T) {
	repo := &stubAlertLister{}
	uc := NewListAlertsUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 50 {
		t.Fatalf("expected default limit 50, got %d", repo.lastLimit)
	}
}

func TestListAlerts_ClampsLimit(t *testing.T) {
	repo := &stubAlertLister{}
	uc := NewListAlertsUseCase(repo)

	if _, err := uc.List(context.Background(), ListInput{TenantID: "t1", Limit: 500}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastLimit != 100 {
		t.Fatalf("expected limit clamped to 100, got %d", repo.lastLimit)
	}
}
