//go:build integration

package postgres_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"orbitjob/internal/core/app/controlplane"
	"orbitjob/internal/core/domain/jobrun"
	store "orbitjob/internal/core/store/postgres"
)

// Reproduce a checkpoint written by the superseded deletion-only protocol.
// The current writer must refuse to manufacture or upgrade such a proof.
func seedDeletionOnlyProof(t *testing.T, f workflowFenceFixture, repo *store.ControlPlaneRepository, e jobrun.Execution, release string) jobrun.RecordedAttempt {
	t.Helper()
	target := recordedAttempt(t, f, repo, e)
	var proof map[string]any
	if err := json.Unmarshal(retainedProof(t, target), &proof); err != nil {
		t.Fatal(err)
	}
	proof["stop_boundary"], proof["hold_release"] = "deleting", release
	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, data); err == nil {
		t.Fatal("current writer accepted deletion-only proof")
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_attempts_control_plane SET termination_evidence=$1::jsonb WHERE tenant_id=$2 AND id=$3`, string(data), f.tenant, target.ID); err != nil {
		t.Fatal(err)
	}
	return recordedAttempt(t, f, repo, e)
}

func TestCancellationRejectsDeletionOnlyRetainedCheckpoint(t *testing.T) {
	for _, release := range []string{"pending", "done"} {
		t.Run(release, func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			e = requestCancellation(t, f, repo, e)
			target := seedDeletionOnlyProof(t, f, repo, e, release)
			if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt); changed || !errors.Is(err, store.ErrRunChanged) {
				t.Fatalf("deletion-only checkpoint confirmed cancellation: %t %v", changed, err)
			}
			if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, retainedProof(t, target)); !errors.Is(err, store.ErrRunChanged) {
				t.Fatalf("old proof silently upgraded: %v", err)
			}
			if err := repo.MarkEvidenceReleased(t.Context(), f.tenant, target); err == nil {
				t.Fatal("unacknowledged proof marked released")
			}
			if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e)); !errors.Is(err, store.ErrRunChanged) {
				t.Fatalf("old protocol fell back to legacy proof: %v", err)
			}
			pending, err := repo.PendingEvidenceAfter(t.Context(), f.tenant, 0, 100)
			if err != nil || len(pending) != 1 || pending[0].ID != target.ID {
				t.Fatalf("old checkpoint escaped recovery: %+v %v", pending, err)
			}
			if !bytes.Equal(target.TerminationEvidence, recordedAttempt(t, f, repo, e).TerminationEvidence) {
				t.Fatal("refused writes changed original proof")
			}
			if err := repo.IsolateAttemptEvidence(t.Context(), f.tenant, target, jobrun.ReasonPodIdentityAmbiguous); err != nil {
				t.Fatal(err)
			}
			isolated := recordedAttempt(t, f, repo, e)
			if isolated.UnknownSince == nil || isolated.Execution.Run.Phase != jobrun.CancelUnknown || !jobrun.EvidenceReleasePending(isolated.TerminationEvidence) {
				t.Fatal("isolation lost retained obligation")
			}
			var proof map[string]any
			if err := json.Unmarshal(isolated.TerminationEvidence, &proof); err != nil {
				t.Fatal(err)
			}
			if proof["stop_boundary"] != "deleting" || proof["revoked"] != true {
				t.Fatal("isolation manufactured controller acknowledgement")
			}
		})
	}
}

func TestDeletionOnlyDoneCheckpointBlocksRetentionAndArchiveCleanup(t *testing.T) {
	for _, kind := range []string{"ordinary", "workflow"} {
		t.Run(kind, func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			if kind == "ordinary" {
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET workflow_run_id=NULL WHERE id=$1`, e.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			target := seedDeletionOnlyProof(t, f, repo, e, "done")
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Succeeded',updated_at=now()-interval '20 days',outcome_recorded_at=now() WHERE id=$1`, e.Run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_attempts_control_plane SET phase='Succeeded' WHERE id=$1`, target.ID); err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkArchivedJobCleaned(t.Context(), f.tenant, controlplane.ArchivedJob{RunID: e.Run.ID, Attempt: target.Number, JobUID: target.JobUID, RunUID: target.OwnerRunUID}); err != nil {
				t.Fatal(err)
			}
			var cleaned sql.NullTime
			if err := f.owner.QueryRowContext(t.Context(), `SELECT log_resources_cleaned_at FROM job_run_attempts_control_plane WHERE id=$1`, target.ID).Scan(&cleaned); err != nil {
				t.Fatal(err)
			}
			if cleaned.Valid {
				t.Fatal("old done checkpoint discharged archive cleanup")
			}
			if kind == "ordinary" {
				runs, err := repo.PrunableRuns(t.Context(), f.tenant, e.SourceUID, 0, 0)
				if err != nil || len(runs) != 0 {
					t.Fatalf("old done entered retention: %+v %v", runs, err)
				}
				if deleted, err := repo.DeleteRun(t.Context(), f.tenant, e.Run.ID); err != nil || deleted {
					t.Fatalf("old done run deleted: %t %v", deleted, err)
				}
			} else {
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='Succeeded',updated_at=now()-interval '20 days' WHERE id=$1`, f.parent.ID); err != nil {
					t.Fatal(err)
				}
				wf := store.NewWorkflowRunRepository(f.application)
				runs, err := wf.PrunableRuns(t.Context(), f.tenant, f.parent.SourceUID, 0, 0)
				if err != nil || len(runs) != 0 {
					t.Fatalf("old done workflow entered retention: %+v %v", runs, err)
				}
				if deleted, n, err := wf.PruneRun(t.Context(), f.tenant, f.parent.ID); err != nil || deleted || n != 0 {
					t.Fatalf("old done workflow pruned: %t %d %v", deleted, n, err)
				}
			}
			pending, err := repo.PendingEvidenceAfter(t.Context(), f.tenant, 0, 100)
			if err != nil || len(pending) != 1 || pending[0].ID != target.ID {
				t.Fatalf("terminal old done checkpoint lost: %+v %v", pending, err)
			}
		})
	}
}
