package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

// The job definition read use cases are thin: validate, then hand the
// normalized input to the store and its result back. The stubs capture what
// crossed the boundary, so these tests pin the contract: nothing reaches the
// store un-normalized, and no store call happens for a refused input.

var errJobStoreDown = errors.New("job store down")

type stubJobGetter struct {
	in    GetInput
	calls int

	item GetItem
	err  error
}

func (s *stubJobGetter) Get(_ context.Context, in GetInput) (GetItem, error) {
	s.calls++
	s.in = in
	return s.item, s.err
}

type stubJobLister struct {
	in    ListInput
	calls int

	items []ListItem
	err   error
}

func (s *stubJobLister) List(_ context.Context, in ListInput) ([]ListItem, error) {
	s.calls++
	s.in = in
	return s.items, s.err
}

func TestGetJobUseCaseHappyPath(t *testing.T) {
	now := time.Now()
	repo := &stubJobGetter{item: GetItem{
		ID: 7, TenantID: ulidTenant, Name: "nightly", SourceUID: "src-nightly",
		Generation: 3, Actor: "key-9", CreatedAt: now,
	}}
	uc := NewGetJobUseCase(repo)

	got, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 7 || got.Name != "nightly" || got.Generation != 3 {
		t.Fatalf("unexpected item: %+v", got)
	}
	if repo.calls != 1 {
		t.Fatalf("repo called %d times, want 1", repo.calls)
	}
	if repo.in.TenantID != ulidTenant || repo.in.ID != 7 {
		t.Fatalf("repo got tenant=%q id=%d, want the normalized input", repo.in.TenantID, repo.in.ID)
	}
}

func TestGetJobUseCaseRefusedInputNeverReachesStore(t *testing.T) {
	repo := &stubJobGetter{}
	uc := NewGetJobUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 0})
	var verr *validation.Error
	if !errors.As(err, &verr) || verr.Field != "id" {
		t.Fatalf("got %v, want a refusal on field %q", err, "id")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}

func TestGetJobUseCaseScopedCallerRefused(t *testing.T) {
	repo := &stubJobGetter{}
	uc := NewGetJobUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 7, ResourceGroupID: "rg-7"})
	if err == nil {
		t.Fatal("expected a scope refusal")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}

func TestGetJobUseCaseStoreErrorPropagates(t *testing.T) {
	repo := &stubJobGetter{err: errJobStoreDown}
	uc := NewGetJobUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 7})
	if !errors.Is(err, errJobStoreDown) {
		t.Fatalf("got %v, want the store error", err)
	}
}

func TestListJobsUseCaseNormalizesBeforeStore(t *testing.T) {
	repo := &stubJobLister{items: []ListItem{{ID: 7, TenantID: ulidTenant, Name: "nightly"}}}
	uc := NewListJobsUseCase(repo)

	got, err := uc.List(context.Background(), ListInput{TenantID: ulidTenant})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("unexpected page: %+v", got)
	}
	if repo.in.Limit != DefaultListLimit {
		t.Fatalf("repo got limit %d, want the default %d", repo.in.Limit, DefaultListLimit)
	}
	if repo.in.TenantID != ulidTenant {
		t.Fatalf("repo got tenant %q, want %q", repo.in.TenantID, ulidTenant)
	}
}

func TestListJobsUseCaseRefusedInputNeverReachesStore(t *testing.T) {
	repo := &stubJobLister{}
	uc := NewListJobsUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: ulidTenant, Limit: MaxListLimit + 1})
	var verr *validation.Error
	if !errors.As(err, &verr) || verr.Field != "limit" {
		t.Fatalf("got %v, want a refusal on field %q", err, "limit")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}

func TestListJobsUseCaseStoreErrorPropagates(t *testing.T) {
	repo := &stubJobLister{err: errJobStoreDown}
	uc := NewListJobsUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: ulidTenant})
	if !errors.Is(err, errJobStoreDown) {
		t.Fatalf("got %v, want the store error", err)
	}
}
