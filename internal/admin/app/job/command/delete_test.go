package command

import (
	"context"
	"errors"
	"testing"

	domainjob "orbitjob/internal/core/domain/job"
)

type stubDeleter struct {
	out domainjob.Snapshot
	err error
}

func (r *stubDeleter) Delete(_ context.Context, _ string, _ int64) (domainjob.Snapshot, error) {
	return r.out, r.err
}

func TestNewDeleteJobUseCase(t *testing.T) {
	uc := NewDeleteJobUseCase(&stubDeleter{})
	if uc == nil {
		t.Fatal("expected use case to be initialized")
	}
}

func TestDeleteJobUseCase_Delete(t *testing.T) {
	repo := &stubDeleter{
		out: domainjob.Snapshot{
			ID:       1,
			Name:     "daily-report",
			TenantID: "default",
			Status:   "deleted",
			Version:  2,
		},
	}
	uc := NewDeleteJobUseCase(repo)

	out, err := uc.Delete(context.Background(), DeleteInput{ID: 1, TenantID: "default"})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if out.ID != 1 {
		t.Fatalf("expected ID=%d, got %d", 1, out.ID)
	}
	if out.Status != "deleted" {
		t.Fatalf("expected Status=%q, got %q", "deleted", out.Status)
	}
}

func TestDeleteJobUseCase_Delete_RepoError(t *testing.T) {
	repo := &stubDeleter{err: errors.New("not found")}
	uc := NewDeleteJobUseCase(repo)

	_, err := uc.Delete(context.Background(), DeleteInput{ID: 999, TenantID: "default"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
