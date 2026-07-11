package command

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincheck "orbitjob/internal/core/domain/check"
)

type fakeClock struct{ t time.Time }

func (f fakeClock) Now() time.Time { return f.t }

type stubCheckCreator struct {
	in  domaincheck.CreateSpec
	out domaincheck.Snapshot
	err error
}

func (s *stubCheckCreator) Create(_ context.Context, spec domaincheck.CreateSpec) (domaincheck.Snapshot, error) {
	s.in = spec
	return s.out, s.err
}

type stubCheckDeleter struct {
	lastTenantID string
	lastID       int64
	lastVersion  int
	err          error
}

func (s *stubCheckDeleter) Delete(_ context.Context, tenantID string, id int64, version int) error {
	s.lastTenantID = tenantID
	s.lastID = id
	s.lastVersion = version
	return s.err
}

type stubCheckStatusChanger struct {
	lastAction string
	out        domaincheck.Snapshot
	err        error
}

func (s *stubCheckStatusChanger) ChangeStatus(_ context.Context, _ string, _ int64, _ int, action string) (domaincheck.Snapshot, error) {
	s.lastAction = action
	return s.out, s.err
}

func intPtr(i int) *int { return &i }

func validCreateInput() CreateInput {
	return CreateInput{
		Name:         "check-1",
		TenantID:     "t1",
		CheckType:    domaincheck.CheckTypeHTTPHealth,
		ScheduleType: domaincheck.ScheduleTypeInterval,
		IntervalSec:  intPtr(60),
		Timezone:     "UTC",
		TimeoutSec:   10,
	}
}

func TestCreateCheck_Success(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	repo := &stubCheckCreator{out: domaincheck.Snapshot{
		ID: 1, Name: "check-1", TenantID: "t1", Status: "active",
		CheckType: domaincheck.CheckTypeHTTPHealth, ScheduleType: domaincheck.ScheduleTypeInterval,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}}
	uc := NewCreateCheckUseCase(repo)
	uc.clock = fakeClock{t: now}

	result, err := uc.Create(context.Background(), validCreateInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 1 || result.Name != "check-1" || result.TenantID != "t1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.in.Name != "check-1" {
		t.Fatalf("expected repo input name=check-1, got %+v", repo.in)
	}
}

func TestCreateCheck_NormalizeError(t *testing.T) {
	repo := &stubCheckCreator{}
	uc := NewCreateCheckUseCase(repo)
	uc.clock = fakeClock{t: time.Now()}

	// Empty name -> NormalizeCreate returns validation error before repo is called.
	_, err := uc.Create(context.Background(), CreateInput{
		Name: "", TenantID: "t1", CheckType: domaincheck.CheckTypeHTTPHealth,
		ScheduleType: domaincheck.ScheduleTypeInterval, IntervalSec: intPtr(60), Timezone: "UTC",
	})
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
	if repo.in.Name != "" {
		t.Fatal("expected repo.Create not called on normalize error")
	}
}

func TestCreateCheck_RepoError(t *testing.T) {
	repo := &stubCheckCreator{err: errors.New("write failed")}
	uc := NewCreateCheckUseCase(repo)
	uc.clock = fakeClock{t: time.Now()}

	_, err := uc.Create(context.Background(), validCreateInput())
	if err == nil {
		t.Fatal("expected repo error")
	}
}

func TestDeleteCheck_Success(t *testing.T) {
	repo := &stubCheckDeleter{}
	uc := NewDeleteCheckUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: "t1", ID: 7, Version: 3}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastTenantID != "t1" || repo.lastID != 7 || repo.lastVersion != 3 {
		t.Fatalf("expected delete t1/7/3, got %s/%d/%d", repo.lastTenantID, repo.lastID, repo.lastVersion)
	}
}

func TestDeleteCheck_RepoError(t *testing.T) {
	repo := &stubCheckDeleter{err: errors.New("not found")}
	uc := NewDeleteCheckUseCase(repo)

	if err := uc.Delete(context.Background(), DeleteInput{TenantID: "t1", ID: 7, Version: 3}); err == nil {
		t.Fatal("expected error")
	}
}

func TestPauseCheck_Success(t *testing.T) {
	repo := &stubCheckStatusChanger{out: domaincheck.Snapshot{ID: 7, Status: "paused", Version: 4}}
	uc := NewPauseCheckUseCase(repo)

	result, err := uc.Pause(context.Background(), ChangeStatusInput{TenantID: "t1", ID: 7, Version: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != 7 || result.Status != "paused" || result.Version != 4 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if repo.lastAction != domaincheck.ActionPause {
		t.Fatalf("expected action pause, got %q", repo.lastAction)
	}
}

func TestPauseCheck_RepoError(t *testing.T) {
	repo := &stubCheckStatusChanger{err: errors.New("stale version")}
	uc := NewPauseCheckUseCase(repo)

	_, err := uc.Pause(context.Background(), ChangeStatusInput{TenantID: "t1", ID: 7, Version: 3})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResumeCheck_Success(t *testing.T) {
	repo := &stubCheckStatusChanger{out: domaincheck.Snapshot{ID: 7, Status: "active", Version: 5}}
	uc := NewResumeCheckUseCase(repo)

	result, err := uc.Resume(context.Background(), ChangeStatusInput{TenantID: "t1", ID: 7, Version: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "active" {
		t.Fatalf("expected active, got %q", result.Status)
	}
	if repo.lastAction != domaincheck.ActionResume {
		t.Fatalf("expected action resume, got %q", repo.lastAction)
	}
}

func TestResumeCheck_RepoError(t *testing.T) {
	repo := &stubCheckStatusChanger{err: errors.New("stale version")}
	uc := NewResumeCheckUseCase(repo)

	_, err := uc.Resume(context.Background(), ChangeStatusInput{TenantID: "t1", ID: 7, Version: 4})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateCheck_DefaultClock(t *testing.T) {
	// Exercise the default realClock set by NewCreateCheckUseCase (not overridden).
	repo := &stubCheckCreator{out: domaincheck.Snapshot{
		ID: 1, Name: "check-1", TenantID: "t1", Status: "active",
		CheckType: domaincheck.CheckTypeHTTPHealth, ScheduleType: domaincheck.ScheduleTypeInterval,
		Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	uc := NewCreateCheckUseCase(repo) // default realClock

	_, err := uc.Create(context.Background(), validCreateInput())
	if err != nil {
		t.Fatalf("unexpected error with default clock: %v", err)
	}
}
