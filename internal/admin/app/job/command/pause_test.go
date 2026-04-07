package command

import (
	"context"
	"errors"
	"testing"
	"time"

	query "orbitjob/internal/admin/app/job/query"
	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
)

type testJobStatusReader struct {
	called bool
	in     query.GetInput
	out    query.GetItem
	err    error
}

func (r *testJobStatusReader) Get(ctx context.Context, in query.GetInput) (query.GetItem, error) {
	r.called = true
	r.in = in
	return r.out, r.err
}

type testJobStatusRepo struct {
	called    bool
	in        domainjob.ChangeStatusSpec
	changedBy string
	out       domainjob.Snapshot
	err       error
}

func (r *testJobStatusRepo) ChangeStatus(
	ctx context.Context,
	in domainjob.ChangeStatusSpec,
	changedBy string,
) (domainjob.Snapshot, error) {
	r.called = true
	r.in = in
	r.changedBy = changedBy
	return r.out, r.err
}

func TestChangeStatusUseCase_Pause(t *testing.T) {
	reader := &testJobStatusReader{
		out: query.GetItem{
			ID:       42,
			TenantID: "tenant-a",
			Status:   domainjob.StatusActive,
		},
	}
	repo := &testJobStatusRepo{
		out: domainjob.Snapshot{
			ID:        42,
			Name:      "nightly-report",
			TenantID:  "tenant-a",
			Status:    domainjob.StatusPaused,
			Version:   5,
			UpdatedAt: time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
		},
	}
	uc := NewChangeStatusUseCase(reader, repo)

	out, err := uc.Pause(context.Background(), ChangeStatusInput{
		ID:        42,
		TenantID:  "tenant-a",
		Version:   4,
		ChangedBy: "control-plane-user",
	})
	if err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if !reader.called {
		t.Fatal("expected reader.Get to be called")
	}
	if !repo.called {
		t.Fatal("expected repo.ChangeStatus to be called")
	}
	if repo.in.CurrentStatus != domainjob.StatusActive {
		t.Fatalf("expected current_status=%q, got %q", domainjob.StatusActive, repo.in.CurrentStatus)
	}
	if repo.in.NextStatus != domainjob.StatusPaused {
		t.Fatalf("expected next_status=%q, got %q", domainjob.StatusPaused, repo.in.NextStatus)
	}
	if repo.in.Action != domainjob.ActionPause {
		t.Fatalf("expected action=%q, got %q", domainjob.ActionPause, repo.in.Action)
	}
	if repo.changedBy != "control-plane-user" {
		t.Fatalf("expected changed_by=%q, got %q", "control-plane-user", repo.changedBy)
	}
	if out.Status != domainjob.StatusPaused {
		t.Fatalf("expected result status=%q, got %q", domainjob.StatusPaused, out.Status)
	}
}

func TestChangeStatusUseCase_Resume(t *testing.T) {
	reader := &testJobStatusReader{
		out: query.GetItem{
			ID:       42,
			TenantID: "tenant-a",
			Status:   domainjob.StatusPaused,
		},
	}
	repo := &testJobStatusRepo{
		out: domainjob.Snapshot{
			ID:       42,
			Name:     "nightly-report",
			TenantID: "tenant-a",
			Status:   domainjob.StatusActive,
			Version:  8,
		},
	}
	uc := NewChangeStatusUseCase(reader, repo)

	out, err := uc.Resume(context.Background(), ChangeStatusInput{
		ID:        42,
		TenantID:  "tenant-a",
		Version:   7,
		ChangedBy: "control-plane-user",
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if out.Status != domainjob.StatusActive {
		t.Fatalf("expected result status=%q, got %q", domainjob.StatusActive, out.Status)
	}
}

func TestChangeStatusUseCase_ValidationError(t *testing.T) {
	reader := &testJobStatusReader{
		out: query.GetItem{
			ID:       42,
			TenantID: "tenant-a",
			Status:   domainjob.StatusPaused,
		},
	}
	repo := &testJobStatusRepo{}
	uc := NewChangeStatusUseCase(reader, repo)

	_, err := uc.Pause(context.Background(), ChangeStatusInput{
		ID:       42,
		TenantID: "tenant-a",
		Version:  4,
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if repo.called {
		t.Fatal("expected repo.ChangeStatus not to be called on validation error")
	}
}

func TestChangeStatusUseCase_GetError(t *testing.T) {
	reader := &testJobStatusReader{
		err: &resource.NotFoundError{
			Resource: "job",
			ID:       42,
		},
	}
	repo := &testJobStatusRepo{}
	uc := NewChangeStatusUseCase(reader, repo)

	_, err := uc.Resume(context.Background(), ChangeStatusInput{
		ID:       42,
		TenantID: "tenant-a",
		Version:  7,
	})
	if err == nil {
		t.Fatal("expected get error, got nil")
	}
	if repo.called {
		t.Fatal("expected repo.ChangeStatus not to be called when get fails")
	}
}

func TestChangeStatusUseCase_RepoError(t *testing.T) {
	repoErr := errors.New("change job status: db down")
	reader := &testJobStatusReader{
		out: query.GetItem{
			ID:       42,
			TenantID: "tenant-a",
			Status:   domainjob.StatusActive,
		},
	}
	repo := &testJobStatusRepo{
		err: repoErr,
	}
	uc := NewChangeStatusUseCase(reader, repo)

	_, err := uc.Pause(context.Background(), ChangeStatusInput{
		ID:       42,
		TenantID: "tenant-a",
		Version:  4,
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error %v, got %v", repoErr, err)
	}
}
