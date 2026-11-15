//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	"orbitjob/internal/core/domain/workflow"
	store "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/scan"
)

func seedObservedExecution(t *testing.T) (workflowFenceFixture, *store.ControlPlaneRepository, jobrun.Execution) {
	t.Helper()
	f := seedAdmissionWorkflow(t)
	step := f.step(t)
	repo := store.NewControlPlaneRepository(f.application)
	name, uid, owner := "oj-"+strings.ToLower(scan.GenerateID()), scan.GenerateID(), scan.GenerateID()
	err := repo.CreateAttemptWithEnsure(t.Context(), f.tenant, step.ID,
		jobrun.Attempt{Number: 1, KubernetesJobName: name, OwnerRunUID: owner, Phase: jobrun.CreatingAttempt},
		func(context.Context) (jobrun.Attempt, error) {
			return jobrun.Attempt{Number: 1, KubernetesJobName: name, KubernetesJobUID: uid, Phase: jobrun.Running, ObservedResourceVersion: "10"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	e, found, err := repo.CurrentExecution(t.Context(), f.tenant, step.ID)
	if err != nil || !found {
		t.Fatalf("seeded execution found=%t err=%v", found, err)
	}
	return f, repo, e
}

func requireExecution(t *testing.T, f workflowFenceFixture, repo *store.ControlPlaneRepository, id int64) jobrun.Execution {
	t.Helper()
	e, found, err := repo.CurrentExecution(t.Context(), f.tenant, id)
	if err != nil || !found {
		t.Fatalf("current execution found=%t err=%v", found, err)
	}
	return e
}

func TestExecutionRecoveryUnknownIsDurableIdempotentAndBlocksParent(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	first := requireExecution(t, f, repo, e.Run.ID)
	if first.Run.Phase != jobrun.ExecutionUnknown || first.Run.AttemptPhase != jobrun.ExecutionUnknown || first.Run.UnknownSince == nil || first.Run.UnknownReason != jobrun.ReasonJobNotFound {
		t.Fatalf("uncertain execution=%+v", first.Run)
	}
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	second := requireExecution(t, f, repo, e.Run.ID)
	if second.Run.UnknownSince == nil || !first.Run.UnknownSince.Equal(*second.Run.UnknownSince) {
		t.Fatal("repeat observation reset first uncertainty time")
	}
	var audits int
	if err := f.owner.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE event_type='execution.unknown' AND resource_id=$1`, fmt.Sprint(e.Run.ID)).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("duplicate uncertainty audit count=%d", audits)
	}
	parent, found, err := store.NewWorkflowRunRepository(f.application).RunByID(t.Context(), f.tenant, f.parent.ID)
	if err != nil || !found || parent.Phase != workflow.PhaseExecutionUnknown {
		t.Fatalf("parent=%+v found=%t err=%v", parent, found, err)
	}
	if e.Run.OwnerRunUID == "" || second.Run.OwnerRunUID != e.Run.OwnerRunUID {
		t.Fatal("reservation lost original owner Run UID")
	}
	pending, err := repo.TerminalOutcomesPending(t.Context(), f.tenant, 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("unknown entered terminal outcomes: %+v err=%v", pending, err)
	}
}

func TestExecutionRecoveryUnknownCannotOverwriteTrustedAttemptTerminal(t *testing.T) {
	for _, phase := range []jobrun.Phase{jobrun.Succeeded, jobrun.Failed, jobrun.Canceled, jobrun.RetryWaiting} {
		t.Run(string(phase), func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_attempts_control_plane SET phase=$1 WHERE run_id=$2 AND attempt_number=1`, phase, e.Run.ID); err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); !errors.Is(err, store.ErrRunChanged) {
				t.Fatalf("terminal overwritten: %v", err)
			}
			got := requireExecution(t, f, repo, e.Run.ID)
			if got.Run.AttemptPhase != phase || got.Run.Phase != jobrun.Running || got.Run.UnknownSince != nil {
				t.Fatalf("trusted terminal changed: %+v", got.Run)
			}
		})
	}
}

func TestExecutionRecoveryCancellationKeepsEvidenceGapAndPriority(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelRequested, e.Run.Attempt); err != nil || !changed {
		t.Fatalf("cancel changed=%t err=%v", changed, err)
	}
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	got := requireExecution(t, f, repo, e.Run.ID)
	if got.Run.Phase != jobrun.CancelRequested || got.Run.UnknownSince == nil || got.Run.AttemptPhase != jobrun.ExecutionUnknown {
		t.Fatalf("cancel erased uncertainty: %+v", got.Run)
	}
	if err := repo.ClearExecutionUnknown(t.Context(), f.tenant, got, e.Run.OwnerRunUID); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("ordinary recovery overwrote cancel: %v", err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Succeeded, e.Run.Attempt); err != nil || changed {
		t.Fatalf("ordinary terminal overwrote cancel: changed=%t err=%v", changed, err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelUnknown, e.Run.Attempt); err != nil || !changed {
		t.Fatalf("unknown cancel changed=%t err=%v", changed, err)
	}
	got = requireExecution(t, f, repo, e.Run.ID)
	if got.Run.UnknownSince == nil || got.Run.UnknownReason != jobrun.ReasonJobNotFound {
		t.Fatal("cancel transition dropped persisted evidence gap")
	}
}

func TestExecutionRecoveryRejectsStaleAttemptSnapshot(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	newName, newUID := "oj-"+strings.ToLower(scan.GenerateID()), scan.GenerateID()
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO job_run_attempts_control_plane(tenant_id,run_id,attempt_number,phase,kubernetes_job_name,kubernetes_job_uid) VALUES($1,$2,2,'Running',$3,$4)`, f.tenant, e.Run.ID, newName, newUID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET attempt=2 WHERE id=$1`, e.Run.ID); err != nil {
		t.Fatal(err)
	}
	proof, _ := json.Marshal(map[string]any{"source": "pods", "job_uid": e.Run.AttemptUID})
	for name, call := range map[string]func() error{
		"unknown": func() error { return repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound) },
		"clear":   func() error { return repo.ClearExecutionUnknown(t.Context(), f.tenant, e, e.Run.OwnerRunUID) },
		"proof":   func() error { return repo.RecordTerminationEvidence(t.Context(), f.tenant, e, proof) },
	} {
		if err := call(); !errors.Is(err, store.ErrRunChanged) {
			t.Fatalf("%s accepted stale attempt: %v", name, err)
		}
	}
	got := requireExecution(t, f, repo, e.Run.ID)
	if got.Run.Attempt != 2 || got.Run.AttemptUID != newUID || got.Run.Phase != jobrun.Running || got.Run.UnknownSince != nil || len(got.Run.TerminationEvidence) != 0 {
		t.Fatalf("new attempt corrupted: %+v", got.Run)
	}
}

func TestExecutionRecoveryLearnsOnlyOneOriginalOwnerAndClearsUnknown(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_attempts_control_plane SET owner_run_uid=NULL WHERE run_id=$1`, e.Run.ID); err != nil {
		t.Fatal(err)
	}
	e = requireExecution(t, f, repo, e.Run.ID)
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	owner := scan.GenerateID()
	if err := repo.ClearExecutionUnknown(t.Context(), f.tenant, e, owner); err != nil {
		t.Fatal(err)
	}
	got := requireExecution(t, f, repo, e.Run.ID)
	if got.Run.Phase != jobrun.Running || got.Run.AttemptPhase != jobrun.ExecutionUnknown || got.Run.OwnerRunUID != owner || got.Run.UnknownSince != nil || got.Run.UnknownReason != "" {
		t.Fatalf("recovered execution=%+v", got.Run)
	}
	if err := repo.ClearExecutionUnknown(t.Context(), f.tenant, got, scan.GenerateID()); !errors.Is(err, store.ErrAttemptOwnership) {
		t.Fatalf("replaced original owner: %v", err)
	}
	if _, _, err := repo.UpdateAttemptPhase(t.Context(), f.tenant, e.Run.AttemptJobName, string(jobrun.Running), e.Run.AttemptUID, "10"); err != nil {
		t.Fatal(err)
	}
	if got = requireExecution(t, f, repo, e.Run.ID); got.Run.AttemptPhase != jobrun.Running {
		t.Fatalf("actual observation did not restore attempt: %+v", got.Run)
	}
}

func TestExecutionRecoverySameResourceVersionCanRestoreTerminalAttempt(t *testing.T) {
	for _, phase := range []jobrun.Phase{jobrun.Succeeded, jobrun.Failed} {
		t.Run(string(phase), func(t *testing.T) {
			f, repo, e := seedObservedExecution(t)
			if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
				t.Fatal(err)
			}
			unknown := requireExecution(t, f, repo, e.Run.ID)
			if err := repo.ClearExecutionUnknown(t.Context(), f.tenant, unknown, e.Run.OwnerRunUID); err != nil {
				t.Fatal(err)
			}
			if got := requireExecution(t, f, repo, e.Run.ID); got.Run.AttemptPhase != jobrun.ExecutionUnknown {
				t.Fatalf("identity recovery guessed attempt Running before observation: %+v", got.Run)
			}
			if _, _, err := repo.UpdateAttemptPhase(t.Context(), f.tenant, e.Run.AttemptJobName, string(phase), e.Run.AttemptUID, "10"); err != nil {
				t.Fatalf("same-RV terminal recovery: %v", err)
			}
			got := requireExecution(t, f, repo, e.Run.ID)
			if got.Run.AttemptPhase != phase || got.Run.UnknownSince != nil {
				t.Fatalf("terminal attempt not recovered: %+v", got.Run)
			}
			if _, _, err := repo.UpdateAttemptPhase(t.Context(), f.tenant, e.Run.AttemptJobName, string(jobrun.Running), e.Run.AttemptUID, "11"); !errors.Is(err, store.ErrStaleObservation) {
				t.Fatalf("restored terminal was overwritten: %v", err)
			}
		})
	}
}

func TestExecutionRecoveryKeysetAndTenantIsolation(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	for _, key := range []string{strings.Repeat("d", 64), strings.Repeat("e", 64)} {
		step, _, err := store.NewWorkflowRunRepository(f.application).CreateStepForTenant(t.Context(), f.tenant, f.parent.ID, workflow.StepRun{SourceUID: f.source, RevisionID: f.revision, OccurrenceKey: key, Trigger: jobrun.Workflow}, 2, "review")
		if err != nil {
			t.Fatal(err)
		}
		name, uid := "oj-"+strings.ToLower(scan.GenerateID()), scan.GenerateID()
		if err := repo.CreateAttemptWithEnsure(t.Context(), f.tenant, step.ID, jobrun.Attempt{Number: 1, KubernetesJobName: name, OwnerRunUID: scan.GenerateID()}, func(context.Context) (jobrun.Attempt, error) {
			return jobrun.Attempt{Number: 1, KubernetesJobName: name, KubernetesJobUID: uid, Phase: jobrun.Running}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	after := int64(0)
	visited := 0
	for {
		page, err := repo.ExecutionRecoveriesAfter(t.Context(), f.tenant, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) != 1 || page[0].Run.ID <= after {
			t.Fatalf("nonmonotone keyset page=%+v after=%d", page, after)
		}
		after = page[0].Run.ID
		visited++
	}
	if visited != 3 {
		t.Fatalf("failing/unknown first row blocked later rows: visited=%d", visited)
	}
	foreign := scan.GenerateID()
	if _, found, err := repo.CurrentExecution(t.Context(), foreign, e.Run.ID); err != nil || found {
		t.Fatalf("foreign lookup found=%t err=%v", found, err)
	}
	if rows, err := repo.ExecutionRecoveriesAfter(t.Context(), foreign, 0, 100); err != nil || len(rows) != 0 {
		t.Fatalf("foreign keyset rows=%d err=%v", len(rows), err)
	}
	if err := repo.MarkExecutionUnknown(t.Context(), foreign, e, jobrun.ReasonJobUIDMismatch); err == nil {
		t.Fatal("foreign write accepted")
	}
	var tenantSetting string
	if err := f.application.QueryRowContext(t.Context(), `SELECT COALESCE(current_setting('app.tenant_id',true),'')`).Scan(&tenantSetting); err != nil || tenantSetting != "" {
		t.Fatalf("pooled tenant setting leaked=%q err=%v", tenantSetting, err)
	}
}

func TestExecutionRecoveryPodProofInvalidationPreservesJobTerminal(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	podProof, _ := json.Marshal(map[string]any{"source": "pods", "job_uid": e.Run.AttemptUID, "pods": []string{scan.GenerateID()}})
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, podProof); err != nil {
		t.Fatal(err)
	}
	if len(requireExecution(t, f, repo, e.Run.ID).Run.TerminationEvidence) == 0 {
		t.Fatal("stop proof was not persisted")
	}
	if err := repo.InvalidatePodTerminationEvidence(t.Context(), f.tenant, e); err != nil {
		t.Fatal(err)
	}
	if len(requireExecution(t, f, repo, e.Run.ID).Run.TerminationEvidence) != 0 {
		t.Fatal("live/ambiguous observation did not clear Pod proof")
	}
	jobProof, _ := json.Marshal(map[string]any{"source": "job", "job_uid": e.Run.AttemptUID, "resource_version": "20"})
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, jobProof); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, podProof); err != nil {
		t.Fatal(err)
	}
	if err := repo.InvalidatePodTerminationEvidence(t.Context(), f.tenant, e); err != nil {
		t.Fatal(err)
	}
	var proof map[string]any
	if err := json.Unmarshal(requireExecution(t, f, repo, e.Run.ID).Run.TerminationEvidence, &proof); err != nil || proof["source"] != "job" {
		t.Fatalf("authoritative terminal proof erased: %+v err=%v", proof, err)
	}
	bad, _ := json.Marshal(map[string]any{"source": "job", "job_uid": scan.GenerateID()})
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, bad); err == nil {
		t.Fatal("foreign Job stop proof accepted")
	}
}

func TestExecutionRecoveryUnknownCannotBeReleasedByHistoryRetention(t *testing.T) {
	for _, phase := range []jobrun.Phase{jobrun.ExecutionUnknown, jobrun.CancelUnknown} {
		t.Run(string(phase), func(t *testing.T) {
			f := seedAdmissionWorkflow(t)
			repo := store.NewControlPlaneRepository(f.application)
			row, _, err := repo.CreateOccurrenceForTenant(t.Context(), f.tenant, jobrun.JobRun{
				SourceUID: f.source, RevisionID: fmt.Sprint(f.revision), OccurrenceKey: strings.Repeat("f", 64), Trigger: jobrun.Manual,
			}, 1, "review")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE job_run_control_plane SET phase=$1 WHERE id=$2`, phase, row.ID); err != nil {
				t.Fatal(err)
			}
			prunable, err := repo.PrunableRuns(t.Context(), f.tenant, f.source, 0, 0)
			if err != nil || len(prunable) != 0 {
				t.Fatalf("unknown listed for pruning: %+v err=%v", prunable, err)
			}
			if deleted, err := repo.DeleteRun(t.Context(), f.tenant, row.ID); err != nil || deleted {
				t.Fatalf("unknown history deleted=%t err=%v", deleted, err)
			}
			if open, err := repo.CountOpenRuns(t.Context(), f.tenant, f.source); err != nil || open != 1 {
				t.Fatalf("unknown source slot released: open=%d err=%v", open, err)
			}
			if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase=$1 WHERE id=$2`, phase, f.parent.ID); err != nil {
				t.Fatal(err)
			}
			workflows := store.NewWorkflowRunRepository(f.application)
			if rows, err := workflows.PrunableRuns(t.Context(), f.tenant, f.parent.SourceUID, 0, 0); err != nil || len(rows) != 0 {
				t.Fatalf("unknown workflow listed: rows=%+v err=%v", rows, err)
			}
			if deleted, _, err := workflows.PruneRun(t.Context(), f.tenant, f.parent.ID); err != nil || deleted {
				t.Fatalf("unknown workflow deleted=%t err=%v", deleted, err)
			}
		})
	}
}

func TestExecutionRecoveryTerminalParentCannotPruneAnUnknownChild(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	if err := repo.MarkExecutionUnknown(t.Context(), f.tenant, e, jobrun.ReasonJobNotFound); err != nil {
		t.Fatal(err)
	}
	// Defensive handling of an inconsistent historical/externally repaired row.
	if _, err := f.owner.ExecContext(t.Context(), `UPDATE workflow_run_control_plane SET phase='Succeeded' WHERE id=$1`, f.parent.ID); err != nil {
		t.Fatal(err)
	}
	deleted, steps, err := store.NewWorkflowRunRepository(f.application).PruneRun(t.Context(), f.tenant, f.parent.ID)
	if err != nil || deleted || steps != 0 {
		t.Fatalf("unknown child pruned with historical terminal parent: deleted=%t steps=%d err=%v", deleted, steps, err)
	}
	if got := requireExecution(t, f, repo, e.Run.ID); got.Run.Phase != jobrun.ExecutionUnknown {
		t.Fatalf("unknown child lost: %+v", got.Run)
	}
}
