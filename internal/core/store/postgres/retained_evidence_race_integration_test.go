//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	store "orbitjob/internal/core/store/postgres"
)

func TestFirstEvidenceGapSerializesBeforeStaleProof(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	e = requestCancellation(t, f, repo, e)
	target := recordedAttempt(t, f, repo, e)
	holderDB, holderPID := admissionConnection(t, f)
	gapDB, gapPID := admissionConnection(t, f)
	writerDB, writerPID := admissionConnection(t, f)
	// admissionConnection deliberately defaults to repeatable read for the
	// admission suite. This test exercises lock-serialized business rejection
	// at read committed, including a writer that waited for a concurrent update.
	for _, db := range []*sql.DB{gapDB, writerDB} {
		if _, err := db.ExecContext(t.Context(), `SET default_transaction_isolation='read committed'`); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	holder, err := holderDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if err := store.WithTenant(ctx, holder, f.tenant); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := holder.QueryRowContext(ctx, `SELECT id FROM job_run_attempts_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, f.tenant, target.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	gapDone := make(chan error, 1)
	go func() {
		gapDone <- store.NewControlPlaneRepository(gapDB).IsolateAttemptEvidence(ctx, f.tenant, target, jobrun.ReasonPodIdentityAmbiguous)
	}()
	// The gap transaction already owns the parent/run and is waiting for its
	// target attempt. The stale writer must wait behind that actual DB lock.
	waitCancellationLock(t, ctx, f, holderPID, gapPID, gapDone)
	proof := retainedProof(t, target)
	proofDone := make(chan error, 1)
	go func() {
		proofDone <- store.NewControlPlaneRepository(writerDB).RecordAttemptTerminationEvidence(ctx, f.tenant, target, proof)
	}()
	waitCancellationLock(t, ctx, f, gapPID, writerPID, proofDone)
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-gapDone; err != nil {
		t.Fatal(err)
	}
	if err := <-proofDone; !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("stale writer crossed first gap: %v", err)
	}
	got := recordedAttempt(t, f, repo, e)
	if got.UnknownSince == nil || got.Execution.Run.Phase != jobrun.CancelUnknown || jobrun.HasPodTerminationEvidence(got.TerminationEvidence, got.JobUID) {
		t.Fatalf("gap isolation not retained: %+v", got)
	}
}

func TestRetainedEvidenceBlocksOrdinaryAndWorkflowRetention(t *testing.T) {
	for _, mode := range []string{"ordinary", "workflow-historical"} {
		t.Run(mode, func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			if mode == "ordinary" {
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET workflow_run_id=NULL WHERE id=$1`, e.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			target := recordedAttempt(t, f, repo, e)
			if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, retainedProof(t, target)); err != nil {
				t.Fatal(err)
			}
			// Reproduce a terminal/stale checkpoint left by an interrupted old
			// observer. New runtime transitions must reject this ordering.
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase='Succeeded',updated_at=now()-interval '20 days',outcome_recorded_at=now() WHERE id=$1`, e.Run.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "workflow-historical" {
				if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid,owner_run_uid) VALUES($1,$2,2,'Succeeded',$3,$4,$5)`, f.tenant, e.Run.ID, target.JobName+"-next", target.JobUID+"-next", target.OwnerRunUID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET attempt=2,max_attempts=2 WHERE id=$1`, e.Run.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='Succeeded',updated_at=now()-interval '20 days' WHERE id=$1`, f.parent.ID); err != nil {
					t.Fatal(err)
				}
			}
			pending, err := repo.PendingEvidenceAfter(t.Context(), f.tenant, 0, 100)
			if err != nil || len(pending) != 1 || pending[0].ID != target.ID {
				t.Fatalf("terminal/historical checkpoint not scanned: %+v %v", pending, err)
			}
			if mode == "workflow-historical" && (pending[0].Number != 1 || pending[0].Execution.Run.Attempt != 2 || pending[0].Execution.Run.AttemptUID == pending[0].JobUID) {
				t.Fatal("target attempt replaced current execution identity")
			}
			if mode == "ordinary" {
				runs, err := repo.PrunableRuns(t.Context(), f.tenant, e.SourceUID, 0, 0)
				if err != nil || len(runs) != 0 {
					t.Fatalf("pending run eligible: %+v %v", runs, err)
				}
				if deleted, err := repo.DeleteRun(t.Context(), f.tenant, e.Run.ID); err != nil || deleted {
					t.Fatalf("pending run deleted: %t %v", deleted, err)
				}
			} else {
				wf := store.NewWorkflowRunRepository(f.application)
				if deleted, n, err := wf.PruneRun(t.Context(), f.tenant, f.parent.ID); err != nil || deleted || n != 0 {
					t.Fatalf("pending child pruned: %t %d %v", deleted, n, err)
				}
			}
			target = pending[0]
			if err := repo.MarkEvidenceReleased(t.Context(), f.tenant, target); err != nil {
				t.Fatal(err)
			}
			if mode == "ordinary" {
				if deleted, err := repo.DeleteRun(t.Context(), f.tenant, e.Run.ID); err != nil || !deleted {
					t.Fatalf("released run not pruned: %t %v", deleted, err)
				}
			} else {
				current := requireExecution(t, f, repo, e.Run.ID)
				if current.Run.Attempt != 2 || current.Run.AttemptUID != target.JobUID+"-next" || current.Run.Phase != jobrun.Succeeded {
					t.Fatal("historical cleanup changed current execution")
				}
				if deleted, n, err := store.NewWorkflowRunRepository(f.application).PruneRun(t.Context(), f.tenant, f.parent.ID); err != nil || !deleted || n != 1 {
					t.Fatalf("released workflow not pruned: %t %d %v", deleted, n, err)
				}
			}
		})
	}
}
