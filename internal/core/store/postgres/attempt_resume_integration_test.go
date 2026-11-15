//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
)

func TestIdentityBoundCreationResumesRunPhaseWithoutAnotherCreate(t *testing.T) {
	db, tenant, id := seedIntegrityRun(t, jobrun.Pending, 0)
	ctx := ctxStore(t)
	repo := NewControlPlaneRepository(db)
	intent := jobrun.Attempt{Number: 1, KubernetesJobName: "late-observed"}
	if err := repo.CreateAttemptWithEnsure(ctx, tenant, id, intent, func(context.Context) (jobrun.Attempt, error) {
		return jobrun.Attempt{}, errors.New("response lost")
	}); err == nil {
		t.Fatal("expected uncertain creation")
	}
	if err := repo.BindAttemptObservation(ctx, tenant, id, 1, intent.KubernetesJobName, "observed-uid", "17"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateAttemptWithEnsure(ctx, tenant, id, intent, func(context.Context) (jobrun.Attempt, error) {
		t.Error("recreated an already observed Job")
		return jobrun.Attempt{}, errors.New("unexpected create")
	}); err != nil {
		t.Fatal(err)
	}
	var phase string
	if err := db.QueryRowContext(ctx, `SELECT phase FROM job_run_control_plane WHERE id=$1`, id).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != string(jobrun.Running) {
		t.Fatalf("confirmed creation remained %s", phase)
	}
}
