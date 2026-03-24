package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/job"
)

type testRepo struct {
	called bool
	in     job.CreateJobSpec
	out    job.Job
	err    error
}

func (r *testRepo) Create(ctx context.Context, in job.CreateJobSpec) (job.Job, error) {
	r.called = true
	r.in = in
	return r.out, r.err
}

type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time {
	return c.t
}

func TestCreateJobUseCase_Create(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 58, 0, 0, time.UTC)
	cronExpr := "0 9 * * *"

	repo := &testRepo{
		out: job.Job{
			ID:       1,
			Name:     "daily-report",
			TenantID: "default",
			Status:   "active",
		},
	}
	uc := &CreateJobUseCase{
		repo:  repo,
		clock: fixedClock{t: now},
	}

	out, err := uc.Create(context.Background(), job.CreateJobInput{
		Name:        "daily-report",
		TriggerType: job.TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
		HandlerPayload: map[string]any{
			"url": "https://example.com/hook",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !repo.called {
		t.Fatalf("expected repo.Create to be called")
	}

	if repo.in.Name != "daily-report" {
		t.Fatalf("expected repo input name=%q, got %q", "daily-report", repo.in.Name)
	}
	if repo.in.TenantID != job.DefaultTenantID {
		t.Fatalf("expected repo input tenant_id=%q, got %q", job.DefaultTenantID, repo.in.TenantID)
	}
	if repo.in.TriggerType != job.TriggerTypeCron {
		t.Fatalf("expected repo input trigger_type=%q, got %q", job.TriggerTypeCron, repo.in.TriggerType)
	}
	if repo.in.Timezone != "Asia/Shanghai" {
		t.Fatalf("expected repo input timezone=%q, got %q", "Asia/Shanghai", repo.in.Timezone)
	}
	if repo.in.NextRunAt == nil {
		t.Fatalf("expected repo input next_run_at to be set")
	}

	wantNextRunAt := time.Date(2026, 3, 18, 1, 0, 0, 0, time.UTC)
	if !repo.in.NextRunAt.Equal(wantNextRunAt) {
		t.Fatalf("expected repo input next_run_at=%s, got %s",
			wantNextRunAt.Format(time.RFC3339),
			repo.in.NextRunAt.Format(time.RFC3339),
		)
	}

	if out.ID != repo.out.ID {
		t.Fatalf("expected out.ID=%d, got %d", repo.out.ID, out.ID)
	}
	if out.Name != repo.out.Name {
		t.Fatalf("expected out.Name=%q, got %q", repo.out.Name, out.Name)
	}
}

func TestNewCreateJobUseCase_CreateValidationError(t *testing.T) {
	repo := &testRepo{}
	uc := NewCreateJobUseCase(repo)

	if uc == nil {
		t.Fatalf("expected use case to be initialized")
	}
	if uc.repo != repo {
		t.Fatalf("expected repo to be stored on use case")
	}
	if uc.clock == nil {
		t.Fatalf("expected clock to be initialized")
	}

	_, err := uc.Create(context.Background(), job.CreateJobInput{
		TriggerType: job.TriggerTypeManual,
		HandlerType: "http",
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if repo.called {
		t.Fatalf("expected repo.Create not to be called on validation error")
	}
}

func TestCreateJobUseCase_CreateRepoError(t *testing.T) {
	now := time.Date(2026, 3, 18, 0, 58, 0, 0, time.UTC)
	cronExpr := "0 9 * * *"
	repoErr := errors.New("insert failed")

	repo := &testRepo{
		err: repoErr,
	}
	uc := &CreateJobUseCase{
		repo:  repo,
		clock: fixedClock{t: now},
	}

	_, err := uc.Create(context.Background(), job.CreateJobInput{
		Name:        "daily-report",
		TriggerType: job.TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error %q, got %v", repoErr, err)
	}
	if !repo.called {
		t.Fatalf("expected repo.Create to be called")
	}
}
