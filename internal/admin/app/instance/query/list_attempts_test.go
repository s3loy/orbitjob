package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubAttemptLister struct {
	items []AttemptItem
	err   error
}

func (r *stubAttemptLister) ListAttempts(_ context.Context, _, _ string) ([]AttemptItem, error) {
	return r.items, r.err
}

func TestNewListAttemptsUseCase(t *testing.T) {
	uc := NewListAttemptsUseCase(&stubAttemptLister{})
	if uc == nil {
		t.Fatal("expected use case to be initialized")
	}
}

func TestListAttemptsUseCase_List(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	workerID := "worker-1"
	resultCode := "0"
	repo := &stubAttemptLister{
		items: []AttemptItem{
			{
				AttemptNo:  1,
				WorkerID:   &workerID,
				Status:     "success",
				StartedAt:  &now,
				FinishedAt: &now,
				ResultCode: &resultCode,
			},
		},
	}
	uc := NewListAttemptsUseCase(repo)

	items, err := uc.List(context.Background(), "tenant-a", "run-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].AttemptNo != 1 {
		t.Fatalf("expected AttemptNo=1, got %d", items[0].AttemptNo)
	}
}

func TestListAttemptsUseCase_List_Empty(t *testing.T) {
	repo := &stubAttemptLister{items: []AttemptItem{}}
	uc := NewListAttemptsUseCase(repo)

	items, err := uc.List(context.Background(), "tenant-a", "run-1")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestListAttemptsUseCase_List_RepoError(t *testing.T) {
	repo := &stubAttemptLister{err: errors.New("db down")}
	uc := NewListAttemptsUseCase(repo)

	_, err := uc.List(context.Background(), "tenant-a", "run-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
