//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
)

func TestChildCancellationUnknownAtomicallyIsolatesWorkflow(t *testing.T) {
	for _, cause := range []string{"missing-after-cancel", "unconfirmed-cancel"} {
		for _, parentPhase := range []workflow.Phase{workflow.PhasePending, workflow.PhaseRunning, workflow.PhaseCancelRequested, workflow.PhaseCancelUnknown, workflow.PhaseSucceeded} {
			t.Run(cause+"/"+string(parentPhase), func(t *testing.T) {
				f, repo, e := seedObservedExecution(t)
				workflows := store.NewWorkflowRunRepository(f.application)
				sibling, _, err := workflows.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{
					SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("e", 64), Trigger: jobrun.Workflow,
				}, 2, "review")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase=$1 WHERE id=$2`, parentPhase, f.parent.ID); err != nil {
					t.Fatal(err)
				}
				if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelRequested, e.Run.Attempt); err != nil || !changed {
					t.Fatalf("cancel intent changed=%t err=%v", changed, err)
				}
				if cause == "missing-after-cancel" {
					err = repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound)
				} else {
					_, err = repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelUnknown, e.Run.Attempt)
				}
				if err != nil {
					t.Fatal(err)
				}
				parent, found, err := workflows.RunByID(t.Context(), f.tenant, f.parent.ID)
				if err != nil || !found {
					t.Fatalf("parent found=%t err=%v", found, err)
				}
				want := parentPhase
				if parentPhase == workflow.PhasePending || parentPhase == workflow.PhaseRunning {
					want = workflow.PhaseExecutionUnknown
				}
				if parent.Phase != want {
					t.Fatalf("child uncertainty committed without parent isolation: got=%s want=%s", parent.Phase, want)
				}
				if want != workflow.PhaseExecutionUnknown {
					return
				}
				if _, _, err := workflows.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{
					SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("d", 64), Trigger: jobrun.Workflow,
				}, 2, "review"); !errors.Is(err, store.ErrWorkflowBlocked) {
					t.Fatalf("new independent step crossed unknown parent: %v", err)
				}
				if err := repo.CreateAttemptWithEnsure(t.Context(), f.tenant, sibling.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "must-not-start"}, func(context.Context) (jobrun.Attempt, error) {
					t.Error("pending sibling invoked Ensure under unknown parent")
					return jobrun.Attempt{}, nil
				}); !errors.Is(err, store.ErrWorkflowBlocked) {
					t.Fatalf("new sibling attempt crossed unknown parent: %v", err)
				}
			})
		}
	}
}

func TestHistoricalUnknownChildFencesNewWorkBeforeParentRollup(t *testing.T) {
	for _, phase := range []jobrun.Phase{jobrun.CancelUnknown, jobrun.ExecutionUnknown} {
		t.Run(string(phase), func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			workflows := store.NewWorkflowRunRepository(f.application)
			siblingRequest := workflow.StepRun{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("e", 64), Trigger: jobrun.Workflow}
			sibling, _, err := workflows.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, siblingRequest, 2, "review")
			if err != nil {
				t.Fatal(err)
			}
			// Old writers could commit this child fact and stop before roll-up.
			// Do not call the new atomic transition helper while seeding it.
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='Running' WHERE id=$1`, f.parent.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase=$1 WHERE id=$2`, phase, e.Run.ID); err != nil {
				t.Fatal(err)
			}
			if replay, created, err := workflows.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, siblingRequest, 2, "review"); err != nil || created || replay.ID != sibling.ID {
				t.Fatalf("existing occurrence replay was blocked: run=%+v created=%t err=%v", replay, created, err)
			}
			if _, created, err := workflows.CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{
				SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: strings.Repeat("d", 64), Trigger: jobrun.Workflow,
			}, 2, "review"); created || !errors.Is(err, store.ErrWorkflowBlocked) {
				t.Fatalf("stale parent accepted independent new step: created=%t err=%v", created, err)
			}
			if err := repo.CreateAttemptWithEnsure(t.Context(), f.tenant, sibling.ID, jobrun.Attempt{Number: 1, KubernetesJobName: "must-not-start"}, func(context.Context) (jobrun.Attempt, error) {
				t.Error("historical unknown child allowed pending sibling Ensure")
				return jobrun.Attempt{}, nil
			}); !errors.Is(err, store.ErrWorkflowBlocked) {
				t.Fatalf("stale parent allowed a new attempt: %v", err)
			}
			if _, _, err := repo.UpdateAttemptPhase(t.Context(), f.tenant, e.Run.AttemptJobName, string(jobrun.Succeeded), e.Run.AttemptUID, "11"); err != nil {
				t.Fatalf("unknown sibling blocked a real execution observation: %v", err)
			}
			var attempts int
			if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM job_run_attempts_control_plane WHERE run_id=$1`, sibling.ID).Scan(&attempts); err != nil || attempts != 0 {
				t.Fatalf("blocked sibling reserved an attempt: count=%d err=%v", attempts, err)
			}
		})
	}
}
