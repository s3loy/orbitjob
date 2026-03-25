package query

import (
	"context"
	"errors"
	"testing"
)

type testJobListReader struct {
	called bool
	in     ListInput
	out    []ListItem
	err    error
}

func (r *testJobListReader) List(ctx context.Context, in ListInput) ([]ListItem, error) {
	r.called = true
	r.in = in
	return r.out, r.err
}

func TestListJobsUseCase_List(t *testing.T) {
	repo := &testJobListReader{
		out: []ListItem{
			{
				ID:     1,
				Name:   "demo-job",
				Status: StatusActive,
			},
		},
	}
	uc := NewListJobsUseCase(repo)

	out, err := uc.List(context.Background(), ListInput{
		TenantID: " tenant-a ",
		Status:   StatusActive,
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
	if repo.in.Status != StatusActive {
		t.Fatalf("expected status=%q, got %q", StatusActive, repo.in.Status)
	}
	if repo.in.Limit != DefaultListLimit {
		t.Fatalf("expected limit=%d, got %d", DefaultListLimit, repo.in.Limit)
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

	_, err := uc.List(context.Background(), ListInput{
		Limit: MaxListLimit + 1,
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

	_, err := uc.List(context.Background(), ListInput{
		Limit: 10,
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error %q, got %v", repoErr, err)
	}
	if !repo.called {
		t.Fatalf("expected repo.List to be called")
	}
}
