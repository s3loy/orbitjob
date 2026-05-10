package command

import (
	"context"
	"errors"
	"testing"

	domaininstance "orbitjob/internal/core/domain/instance"
)

type stubCancelReader struct {
	snapshot domaininstance.Snapshot
	err      error
}

func (r *stubCancelReader) GetByRunID(_ context.Context, _ string) (domaininstance.Snapshot, error) {
	return r.snapshot, r.err
}

type stubCancelRepo struct {
	out domaininstance.Snapshot
	err error
}

func (r *stubCancelRepo) Cancel(_ context.Context, _ string, _ int) (domaininstance.Snapshot, error) {
	return r.out, r.err
}

func TestNewCancelInstanceUseCase(t *testing.T) {
	uc := NewCancelInstanceUseCase(&stubCancelReader{}, &stubCancelRepo{})
	if uc == nil {
		t.Fatal("expected use case to be initialized")
	}
}

func TestCancelInstanceUseCase_Cancel_Dispatched(t *testing.T) {
	reader := &stubCancelReader{
		snapshot: domaininstance.Snapshot{
			RunID:   "run-1",
			Status:  domaininstance.StatusDispatched,
			Version: 1,
		},
	}
	repo := &stubCancelRepo{
		out: domaininstance.Snapshot{
			RunID:   "run-1",
			Status:  domaininstance.StatusCanceled,
			Version: 2,
		},
	}
	uc := NewCancelInstanceUseCase(reader, repo)

	out, err := uc.Cancel(context.Background(), CancelInstanceInput{RunID: "run-1", Version: 1})
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if out.Status != domaininstance.StatusCanceled {
		t.Fatalf("expected status=%q, got %q", domaininstance.StatusCanceled, out.Status)
	}
}

func TestCancelInstanceUseCase_Cancel_Running(t *testing.T) {
	reader := &stubCancelReader{
		snapshot: domaininstance.Snapshot{
			RunID:   "run-1",
			Status:  domaininstance.StatusRunning,
			Version: 1,
		},
	}
	repo := &stubCancelRepo{
		out: domaininstance.Snapshot{RunID: "run-1", Status: domaininstance.StatusCanceled, Version: 2},
	}
	uc := NewCancelInstanceUseCase(reader, repo)

	out, err := uc.Cancel(context.Background(), CancelInstanceInput{RunID: "run-1", Version: 1})
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if out.Status != domaininstance.StatusCanceled {
		t.Fatalf("expected status=canceled, got %q", out.Status)
	}
}

func TestCancelInstanceUseCase_Cancel_ReaderError(t *testing.T) {
	reader := &stubCancelReader{err: errors.New("db down")}
	repo := &stubCancelRepo{}
	uc := NewCancelInstanceUseCase(reader, repo)

	_, err := uc.Cancel(context.Background(), CancelInstanceInput{RunID: "run-1", Version: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCancelInstanceUseCase_Cancel_InvalidStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"pending", domaininstance.StatusPending},
		{"retry_wait", domaininstance.StatusRetryWait},
		{"success", domaininstance.StatusSuccess},
		{"failed", domaininstance.StatusFailed},
		{"canceled", domaininstance.StatusCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubCancelReader{
				snapshot: domaininstance.Snapshot{RunID: "run-1", Status: tt.status, Version: 1},
			}
			repo := &stubCancelRepo{}
			uc := NewCancelInstanceUseCase(reader, repo)

			_, err := uc.Cancel(context.Background(), CancelInstanceInput{RunID: "run-1", Version: 1})
			if err == nil {
				t.Fatalf("expected error for status %q, got nil", tt.status)
			}
		})
	}
}

func TestCancelInstanceUseCase_Cancel_RepoError(t *testing.T) {
	reader := &stubCancelReader{
		snapshot: domaininstance.Snapshot{RunID: "run-1", Status: domaininstance.StatusDispatched, Version: 1},
	}
	repo := &stubCancelRepo{err: errors.New("cancel failed")}
	uc := NewCancelInstanceUseCase(reader, repo)

	_, err := uc.Cancel(context.Background(), CancelInstanceInput{RunID: "run-1", Version: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
