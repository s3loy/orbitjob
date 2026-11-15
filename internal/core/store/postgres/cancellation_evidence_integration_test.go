//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"orbitjob/internal/core/domain/jobrun"
	store "orbitjob/internal/core/store/postgres"
)

func cancelPodEvidence(e jobrun.Execution) []byte {
	proof, _ := json.Marshal(map[string]any{"source": "pods", "job_uid": e.Run.AttemptUID,
		"pods": []any{map[string]any{"uid": "original-pod", "containers": []any{map[string]any{"name": "task", "state": map[string]any{"terminated": map[string]any{"finishedAt": "2026-10-29T00:00:00Z"}}}}}}})
	return proof
}
func cancelJobEvidence(e jobrun.Execution) []byte {
	proof, _ := json.Marshal(map[string]any{"source": "job", "job_uid": e.Run.AttemptUID, "resource_version": "20"})
	return proof
}
func requestCancellation(t *testing.T, f workflowFenceFixture, repo *store.ControlPlaneRepository, e jobrun.Execution) jobrun.Execution {
	t.Helper()
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelRequested, e.Run.Attempt); err != nil || !changed {
		t.Fatalf("request cancellation changed=%t err=%v", changed, err)
	}
	return requireExecution(t, f, repo, e.Run.ID)
}

func TestCancellationPhaseClockSurvivesRepeatedRequestsAndAttemptObservations(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	e = requestCancellation(t, f, repo, e)
	if e.PhaseUpdatedAt.IsZero() {
		t.Fatal("missing committed request timestamp")
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelRequested, e.Run.Attempt); err != nil || changed {
		t.Fatalf("repeat request changed=%t err=%v", changed, err)
	}
	if _, _, err := repo.UpdateAttemptPhase(t.Context(), f.tenant, e.Run.AttemptJobName, string(jobrun.Failed), e.Run.AttemptUID, "20"); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Failed, e.Run.Attempt); err != nil || changed {
		t.Fatalf("terminal observer changed cancellation=%t err=%v", changed, err)
	}
	got := requireExecution(t, f, repo, e.Run.ID)
	if !e.PhaseUpdatedAt.Equal(got.PhaseUpdatedAt) || got.Run.Phase != jobrun.CancelRequested {
		t.Fatal("repeated request/terminal observation reset cancellation budget")
	}
}

func TestCancellationRejectsJobProofAndReplacesHistoricalJobProofWithPods(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelJobEvidence(e)); err != nil {
		t.Fatal(err)
	}
	stale := e
	e = requestCancellation(t, f, repo, e)
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, stale, cancelJobEvidence(stale)); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("stale pre-cancel writer accepted Job proof: %v", err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt); changed || !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("legacy Job proof released cancellation: %t %v", changed, err)
	}
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e)); err != nil {
		t.Fatal(err)
	}
	got := requireExecution(t, f, repo, e.Run.ID)
	if !jobrun.HasPodTerminationEvidence(got.Run.TerminationEvidence, e.Run.AttemptUID) {
		t.Fatal("validated Pods did not replace historical Job-only proof")
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt); err != nil || !changed {
		t.Fatalf("trusted proof did not complete cancellation: %t %v", changed, err)
	}
}

func TestCancellationUnknownProofBoundaries(t *testing.T) {
	for _, mode := range []string{"unknown-since", "unknown-without-proof", "timeout-with-proof", "contradicted-proof"} {
		t.Run(mode, func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			e = requestCancellation(t, f, repo, e)
			if mode == "timeout-with-proof" || mode == "contradicted-proof" {
				if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unknown-since" {
				if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "contradicted-proof" {
				if err := repo.InvalidatePodTerminationEvidence(t.Context(), f.tenant, e); err != nil {
					t.Fatal(err)
				}
				got := requireExecution(t, f, repo, e.Run.ID)
				if got.Run.UnknownSince == nil || got.Run.Phase != jobrun.CancelUnknown {
					t.Fatal("proof contradiction did not persist isolation")
				}
			} else if _, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelUnknown, e.Run.Attempt); err != nil {
				t.Fatal(err)
			}
			err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e))
			if mode == "timeout-with-proof" {
				if err != nil {
					t.Fatal(err)
				}
				if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt); err != nil || !changed {
					t.Fatalf("retained trusted proof: changed=%t err=%v", changed, err)
				}
			} else {
				if !errors.Is(err, store.ErrRunChanged) {
					t.Fatalf("later narrow proof cleared uncertainty: %v", err)
				}
				if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt); changed || !errors.Is(err, store.ErrRunChanged) {
					t.Fatalf("uncertainty released: changed=%t err=%v", changed, err)
				}
			}
		})
	}
}

// Observe an actual PostgreSQL row-lock wait before committing the competing
// transition. Sleeps alone do not establish that the stale writer raced it.
func waitCancellationLock(t *testing.T, ctx context.Context, f workflowFenceFixture, holder, waiter int, done <-chan error) {
	t.Helper()
	for {
		select {
		case err := <-done:
			t.Fatalf("writer crossed held run lock: %v", err)
		default:
		}
		var blocked bool
		if err := f.owner.QueryRowContext(ctx, `SELECT $1=ANY(pg_blocking_pids($2))`, holder, waiter).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestCancellationLockRejectsConcurrentJobProofAndRevokedCompletion(t *testing.T) {
	for _, mode := range []string{"job-proof-after-cancel", "completion-after-revocation"} {
		t.Run(mode, func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET workflow_run_id=NULL WHERE id=$1`, e.Run.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "completion-after-revocation" {
				e = requestCancellation(t, f, repo, e)
				if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e)); err != nil {
					t.Fatal(err)
				}
			}
			holderDB, holderPID := admissionConnection(t, f)
			writerDB, writerPID := admissionConnection(t, f)
			if _, err := writerDB.ExecContext(t.Context(), `SET default_transaction_isolation='read committed'`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			tx, err := holderDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = store.WithTenant(ctx, tx, f.tenant); err != nil {
				t.Fatal(err)
			}
			var id int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM job_run_control_plane WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, f.tenant, e.Run.ID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				writer := store.NewControlPlaneRepository(writerDB)
				if mode == "job-proof-after-cancel" {
					done <- writer.RecordTerminationEvidence(ctx, f.tenant, e, cancelJobEvidence(e))
				} else {
					_, err := writer.UpdateRunPhase(ctx, f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt)
					done <- err
				}
			}()
			waitCancellationLock(t, ctx, f, holderPID, writerPID, done)
			if mode == "job-proof-after-cancel" {
				_, err = tx.ExecContext(ctx, `UPDATE job_run_control_plane SET phase='CancelRequested',updated_at=now() WHERE tenant_id=$1 AND id=$2`, f.tenant, e.Run.ID)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE job_run_attempts_control_plane SET termination_evidence=NULL WHERE tenant_id=$1 AND run_id=$2`, f.tenant, e.Run.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if !errors.Is(err, store.ErrRunChanged) {
					t.Fatalf("stale transaction passed current locked state: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			got := requireExecution(t, f, repo, e.Run.ID)
			if got.Run.Phase != jobrun.CancelRequested || len(got.Run.TerminationEvidence) != 0 {
				t.Fatal("race fabricated cancellation proof or completed run")
			}
		})
	}
}

func TestCancellationCompletedRunKeepsIdempotentRepeat(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	e = requestCancellation(t, f, repo, e)
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, e.Run.Attempt)
		if err != nil || changed != (i == 0) {
			t.Fatalf("repeat %d: changed=%t err=%v", i, changed, err)
		}
	}
}
