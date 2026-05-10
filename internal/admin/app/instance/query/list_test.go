package query

import (
	"context"
	"errors"
	"testing"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type stubLister struct {
	snapshots []domaininstance.Snapshot
	err       error
}

func (r *stubLister) List(_ context.Context, _, _ string, _, _ int) ([]domaininstance.Snapshot, error) {
	return r.snapshots, r.err
}

func TestNewListInstancesUseCase(t *testing.T) {
	uc := NewListInstancesUseCase(&stubLister{})
	if uc == nil {
		t.Fatal("expected use case to be initialized")
	}
}

func TestListInstancesUseCase_List(t *testing.T) {
	repo := &stubLister{
		snapshots: []domaininstance.Snapshot{
			{RunID: "run-1", Status: domaininstance.StatusRunning, JobID: 1},
			{RunID: "run-2", Status: domaininstance.StatusPending, JobID: 1},
		},
	}
	uc := NewListInstancesUseCase(repo)

	items, err := uc.List(context.Background(), ListInstancesInput{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].RunID != "run-1" {
		t.Fatalf("expected first RunID=%q, got %q", "run-1", items[0].RunID)
	}
}

func TestListInstancesUseCase_List_Empty(t *testing.T) {
	repo := &stubLister{snapshots: []domaininstance.Snapshot{}}
	uc := NewListInstancesUseCase(repo)

	items, err := uc.List(context.Background(), ListInstancesInput{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestListInstancesUseCase_List_LimitClamping(t *testing.T) {
	repo := &stubLister{snapshots: []domaininstance.Snapshot{}}
	uc := NewListInstancesUseCase(repo)

	tests := []struct {
		name  string
		limit int
	}{
		{"zero", 0},
		{"negative", -1},
		{"above_max", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uc.List(context.Background(), ListInstancesInput{Limit: tt.limit})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
		})
	}
}

func TestListInstancesUseCase_List_NegativeOffset(t *testing.T) {
	repo := &stubLister{snapshots: []domaininstance.Snapshot{}}
	uc := NewListInstancesUseCase(repo)

	_, err := uc.List(context.Background(), ListInstancesInput{Limit: 10, Offset: -5})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestListInstancesUseCase_List_RepoError(t *testing.T) {
	repo := &stubLister{err: errors.New("db down")}
	uc := NewListInstancesUseCase(repo)

	_, err := uc.List(context.Background(), ListInstancesInput{Limit: 10})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
