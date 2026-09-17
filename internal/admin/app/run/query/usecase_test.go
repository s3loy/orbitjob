package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

// The run read use cases are thin: validate, then hand the normalized input to
// the store and its result back. The stubs capture what actually crossed the
// boundary, so these tests pin the contract the store can rely on: nothing
// reaches the store un-normalized, and no store call happens for a refused
// input.

var errRunStoreDown = errors.New("run store down")

type stubRunGetter struct {
	in    GetInput
	calls int

	item GetItem
	err  error
}

func (s *stubRunGetter) Get(_ context.Context, in GetInput) (GetItem, error) {
	s.calls++
	s.in = in
	return s.item, s.err
}

type stubRunLister struct {
	in    ListInput
	calls int

	items []ListItem
	err   error
}

func (s *stubRunLister) List(_ context.Context, in ListInput) ([]ListItem, error) {
	s.calls++
	s.in = in
	return s.items, s.err
}

type stubAttemptLister struct {
	in    GetInput
	calls int

	attempts []AttemptItem
	err      error
}

func (s *stubAttemptLister) ListAttempts(_ context.Context, in GetInput) ([]AttemptItem, error) {
	s.calls++
	s.in = in
	return s.attempts, s.err
}

func TestGetRunUseCaseHappyPath(t *testing.T) {
	now := time.Now()
	repo := &stubRunGetter{item: GetItem{
		ID: 7, TenantID: ulidTenant, Phase: "running", Attempt: 2, MaxAttempts: 3,
		Attempts:  []AttemptItem{{ID: 1, RunID: 7, AttemptNumber: 1, Phase: "failed", CreatedAt: now}},
		CreatedAt: now, UpdatedAt: now,
	}}
	uc := NewGetRunUseCase(repo)

	got, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 7 || got.TenantID != ulidTenant || got.Phase != "running" {
		t.Fatalf("unexpected item: %+v", got)
	}
	if len(got.Attempts) != 1 || got.Attempts[0].AttemptNumber != 1 {
		t.Fatalf("attempt trail did not survive: %+v", got.Attempts)
	}
	if repo.calls != 1 {
		t.Fatalf("repo called %d times, want 1", repo.calls)
	}
	if repo.in.TenantID != ulidTenant || repo.in.ID != 7 {
		t.Fatalf("repo got tenant=%q id=%d, want the normalized input", repo.in.TenantID, repo.in.ID)
	}
}

func TestGetRunUseCaseRefusedInputNeverReachesStore(t *testing.T) {
	repo := &stubRunGetter{}
	uc := NewGetRunUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 0})
	var verr *validation.Error
	if !errors.As(err, &verr) {
		t.Fatalf("got %v (%T), want *validation.Error", err, err)
	}
	if verr.Field != "id" {
		t.Fatalf("refusal names field %q, want %q", verr.Field, "id")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}

func TestGetRunUseCaseScopedCallerRefused(t *testing.T) {
	repo := &stubRunGetter{}
	uc := NewGetRunUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 7, ResourceGroupID: "rg-7"})
	if err == nil {
		t.Fatal("expected a scope refusal")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}

func TestGetRunUseCaseStoreErrorPropagates(t *testing.T) {
	repo := &stubRunGetter{err: errRunStoreDown}
	uc := NewGetRunUseCase(repo)

	_, err := uc.Get(context.Background(), GetInput{TenantID: ulidTenant, ID: 7})
	if !errors.Is(err, errRunStoreDown) {
		t.Fatalf("got %v, want the store error", err)
	}
}

func TestListRunsUseCaseNormalizesBeforeStore(t *testing.T) {
	repo := &stubRunLister{items: []ListItem{{ID: 7, TenantID: ulidTenant, Phase: "running"}}}
	uc := NewListRunsUseCase(repo)

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

func TestListRunsUseCaseRefusedInputNeverReachesStore(t *testing.T) {
	repo := &stubRunLister{}
	uc := NewListRunsUseCase(repo)

	// An unknown phase is a typo; the gate must refuse it before it becomes a
	// silently empty result.
	_, err := uc.List(context.Background(), ListInput{TenantID: ulidTenant, Phase: "runing"})
	var verr *validation.Error
	if !errors.As(err, &verr) {
		t.Fatalf("got %v (%T), want *validation.Error", err, err)
	}
	if verr.Field != "phase" {
		t.Fatalf("refusal names field %q, want %q", verr.Field, "phase")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}

func TestListRunsUseCaseStoreErrorPropagates(t *testing.T) {
	repo := &stubRunLister{err: errRunStoreDown}
	uc := NewListRunsUseCase(repo)

	_, err := uc.List(context.Background(), ListInput{TenantID: ulidTenant})
	if !errors.Is(err, errRunStoreDown) {
		t.Fatalf("got %v, want the store error", err)
	}
}

func TestListAttemptsUseCaseHappyPath(t *testing.T) {
	now := time.Now()
	repo := &stubAttemptLister{attempts: []AttemptItem{
		{ID: 1, RunID: 7, AttemptNumber: 1, Phase: "failed", KubernetesJobName: "job-abc-11223344", CreatedAt: now},
		{ID: 2, RunID: 7, AttemptNumber: 2, Phase: "running", KubernetesJobName: "job-abc-55667788", CreatedAt: now},
	}}
	uc := NewListAttemptsUseCase(repo)

	got, err := uc.List(context.Background(), GetInput{TenantID: ulidTenant, ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].AttemptNumber != 1 || got[1].AttemptNumber != 2 {
		t.Fatalf("unexpected trail: %+v", got)
	}
	if repo.in.TenantID != ulidTenant || repo.in.ID != 7 {
		t.Fatalf("repo got tenant=%q id=%d, want the normalized input", repo.in.TenantID, repo.in.ID)
	}
}

func TestListAttemptsUseCaseEmptyTrailIsNotAnError(t *testing.T) {
	repo := &stubAttemptLister{attempts: []AttemptItem{}}
	uc := NewListAttemptsUseCase(repo)

	got, err := uc.List(context.Background(), GetInput{TenantID: ulidTenant, ID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %+v, want an empty trail", got)
	}
}

func TestListAttemptsUseCaseRefusedInputNeverReachesStore(t *testing.T) {
	repo := &stubAttemptLister{}
	uc := NewListAttemptsUseCase(repo)

	_, err := uc.List(context.Background(), GetInput{TenantID: ulidTenant, ID: -1})
	if err == nil {
		t.Fatal("expected a refusal for a negative id")
	}
	if repo.calls != 0 {
		t.Fatalf("refused input reached the store %d times", repo.calls)
	}
}
