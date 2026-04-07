package query

import (
	"context"
	"errors"
	"testing"
)

type testJobGetReader struct {
	called bool
	in     GetInput
	out    GetItem
	err    error
}

func (r *testJobGetReader) Get(ctx context.Context, in GetInput) (GetItem, error) {
	r.called = true
	r.in = in
	return r.out, r.err
}

func TestGetJobUseCase_Get(t *testing.T) {
	repo := &testJobGetReader{
		out: GetItem{
			ID:       1,
			Name:     "demo-job",
			TenantID: "tenant-a",
			Version:  3,
			Status:   StatusActive,
		},
	}
	uc := NewGetJobUseCase(repo)

	out, err := uc.Get(context.Background(), GetInput{
		ID:       1,
		TenantID: " tenant-a ",
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !repo.called {
		t.Fatalf("expected repo.Get to be called")
	}
	if repo.in.ID != 1 {
		t.Fatalf("expected id=%d, got %d", 1, repo.in.ID)
	}
	if repo.in.TenantID != "tenant-a" {
		t.Fatalf("expected tenant_id=%q, got %q", "tenant-a", repo.in.TenantID)
	}
	if out.Version != 3 {
		t.Fatalf("expected version=%d, got %d", 3, out.Version)
	}
}

func TestGetJobUseCase_GetValidationError(t *testing.T) {
	repo := &testJobGetReader{}
	uc := NewGetJobUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if repo.called {
		t.Fatalf("expected repo.Get not to be called on validation error")
	}
}

func TestGetJobUseCase_GetRepoError(t *testing.T) {
	repoErr := errors.New("query failed")
	repo := &testJobGetReader{
		err: repoErr,
	}
	uc := NewGetJobUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{
		ID: 10,
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error %q, got %v", repoErr, err)
	}
	if !repo.called {
		t.Fatalf("expected repo.Get to be called")
	}
}
