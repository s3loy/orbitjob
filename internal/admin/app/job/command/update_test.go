package command

import (
	"context"
	"errors"
	"testing"
	"time"

	domainjob "orbitjob/internal/core/domain/job"
	"orbitjob/internal/domain/resource"
)

type testUpdateRepo struct {
	called    bool
	in        domainjob.UpdateSpec
	changedBy string
	out       domainjob.Snapshot
	err       error
}

func (r *testUpdateRepo) Update(
	ctx context.Context,
	in domainjob.UpdateSpec,
	changedBy string,
) (domainjob.Snapshot, error) {
	r.called = true
	r.in = in
	r.changedBy = changedBy
	return r.out, r.err
}

func TestUpdateJobUseCase_Update(t *testing.T) {
	now := time.Date(2026, 4, 7, 0, 58, 0, 0, time.UTC)
	cronExpr := "0 9 * * *"

	repo := &testUpdateRepo{
		out: domainjob.Snapshot{
			ID:       42,
			Name:     "daily-report",
			TenantID: "tenant-a",
			Status:   "active",
			Version:  5,
		},
	}
	uc := &UpdateJobUseCase{
		repo:  repo,
		clock: fixedClock{t: now},
	}

	out, err := uc.Update(context.Background(), UpdateInput{
		ID:          42,
		TenantID:    "tenant-a",
		ChangedBy:   "control-plane-user",
		Version:     4,
		Name:        "daily-report",
		TriggerType: domainjob.TriggerTypeCron,
		CronExpr:    &cronExpr,
		Timezone:    "Asia/Shanghai",
		HandlerType: "http",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !repo.called {
		t.Fatalf("expected repo.Update to be called")
	}
	if repo.in.ID != 42 {
		t.Fatalf("expected repo input id=%d, got %d", 42, repo.in.ID)
	}
	if repo.in.Version != 4 {
		t.Fatalf("expected repo input version=%d, got %d", 4, repo.in.Version)
	}
	if repo.changedBy != "control-plane-user" {
		t.Fatalf("expected changed_by=%q, got %q", "control-plane-user", repo.changedBy)
	}
	if out.Version != 5 {
		t.Fatalf("expected out.Version=%d, got %d", 5, out.Version)
	}
}

func TestUpdateJobUseCase_UpdateValidationError(t *testing.T) {
	repo := &testUpdateRepo{}
	uc := NewUpdateJobUseCase(repo)

	_, err := uc.Update(context.Background(), UpdateInput{
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "http",
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if repo.called {
		t.Fatalf("expected repo.Update not to be called on validation error")
	}
}

func TestUpdateJobUseCase_UpdateRepoError(t *testing.T) {
	now := time.Date(2026, 4, 7, 0, 58, 0, 0, time.UTC)
	repoErr := &resource.ConflictError{
		Resource: "job",
		Field:    "version",
		Message:  "stale job version",
	}

	repo := &testUpdateRepo{
		err: repoErr,
	}
	uc := &UpdateJobUseCase{
		repo:  repo,
		clock: fixedClock{t: now},
	}

	_, err := uc.Update(context.Background(), UpdateInput{
		ID:          42,
		Version:     4,
		Name:        "daily-report",
		TriggerType: domainjob.TriggerTypeManual,
		HandlerType: "http",
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error %q, got %v", repoErr, err)
	}
	if !repo.called {
		t.Fatalf("expected repo.Update to be called")
	}
}
