package query

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type stubGetter struct {
	snapshot domaininstance.Snapshot
	err      error
}

func (r *stubGetter) GetByRunID(_ context.Context, _ string) (domaininstance.Snapshot, error) {
	return r.snapshot, r.err
}

func TestNewGetInstanceUseCase(t *testing.T) {
	uc := NewGetInstanceUseCase(&stubGetter{})
	if uc == nil {
		t.Fatal("expected use case to be initialized")
	}
}

func TestGetInstanceUseCase_Get(t *testing.T) {
	repo := &stubGetter{
		snapshot: domaininstance.Snapshot{
			RunID:   "run-1",
			JobID:   42,
			Status:  domaininstance.StatusRunning,
			Version: 1,
		},
	}
	uc := NewGetInstanceUseCase(repo)

	item, err := uc.Get(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if item.RunID != "run-1" {
		t.Fatalf("expected RunID=%q, got %q", "run-1", item.RunID)
	}
	if item.JobID != 42 {
		t.Fatalf("expected JobID=%d, got %d", 42, item.JobID)
	}
}

func TestGetInstanceUseCase_Get_NotFound(t *testing.T) {
	repo := &stubGetter{err: sql.ErrNoRows}
	uc := NewGetInstanceUseCase(repo)

	_, err := uc.Get(context.Background(), "run-missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetInstanceUseCase_Get_RepoError(t *testing.T) {
	repo := &stubGetter{err: errors.New("connection refused")}
	uc := NewGetInstanceUseCase(repo)

	_, err := uc.Get(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
