package query

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/job"
)

type testJobListReader struct {
	called bool
	in     job.ListJobsQuery
	out    []job.JobListItem
	err    error
}

func (r *testJobListReader) List(ctx context.Context, in job.ListJobsQuery) ([]job.JobListItem, error) {
	r.called = true
	r.in = in
	return r.out, r.err
}

func TestListJobsUseCase_List(t *testing.T) {
	repo := &testJobListReader{
		out: []job.JobListItem{
			{
				ID:     1,
				Name:   "demo-job",
				Status: job.JobStatusActive,
			},
		},
	}
	uc := NewListJobsUseCase(repo)

	out, err := uc.List(context.Background(), job.ListJobsQuery{
		TenantID: " tenant-a ",
		Status:   job.JobStatusActive,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !repo.called {
		t.Fatalf("expected repo.List to be called")
	}
	if repo.in.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", repo.in.TenantID)
	}
	if repo.in.Status != job.JobStatusActive {
		t.Fatalf("expected status=%q, got %q", job.JobStatusActive, repo.in.Status)
	}
	if repo.in.Limit != job.DefaultListJobsLimit {
		t.Fatalf("expected limit=%d, got %d", job.DefaultListJobsLimit, repo.in.Limit)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	if out[0].ID != 1 {
		t.Fatalf("expected item id=1, got %d", out[0].ID)
	}
}

func TestListJobsUseCase_ListValidationError(t *testing.T) {
	repo := &testJobListReader{}
	uc := NewListJobsUseCase(repo)

	_, err := uc.List(context.Background(), job.ListJobsQuery{
		Limit: job.MaxListJobsLimit + 1,
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if repo.called {
		t.Fatalf("expected repo.List not to be called on validation error")
	}
}

func TestListJobsUseCase_ListRepoError(t *testing.T) {
	repoErr := errors.New("query failed")
	repo := &testJobListReader{
		err: repoErr,
	}
	uc := NewListJobsUseCase(repo)

	_, err := uc.List(context.Background(), job.ListJobsQuery{
		Limit: 10,
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error %q, got %v", repoErr, err)
	}
	if !repo.called {
		t.Fatalf("expected repo.List to be called")
	}
}
