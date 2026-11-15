//go:build integration

package postgres_test

import (
	"encoding/json"
	"errors"
	"testing"

	"orbitjob/internal/core/domain/jobrun"
	store "orbitjob/internal/core/store/postgres"
)

func recordedAttempt(t *testing.T, f workflowFenceFixture, repo *store.ControlPlaneRepository, e jobrun.Execution) jobrun.RecordedAttempt {
	t.Helper()
	target, found, err := repo.RecordedExecution(t.Context(), f.tenant, e.Namespace, e.Run.AttemptJobName, e.Run.AttemptUID)
	if err != nil || !found {
		t.Fatalf("recorded attempt found=%t err=%v", found, err)
	}
	return target
}

func retainedProof(t *testing.T, target jobrun.RecordedAttempt) []byte {
	t.Helper()
	proof := map[string]any{"source": "pods", "protocol": jobrun.PodHoldProtocol, "hold_release": "pending",
		"job_uid": target.JobUID, "job_name": target.JobName, "namespace": target.Execution.Namespace, "stop_boundary": "failure_target",
		"pods": []any{map[string]any{"uid": "original-pod", "name": "pod", "resource_version": "20",
			"containers": []any{map[string]any{"name": "task", "state": map[string]any{"terminated": map[string]any{"finishedAt": "2026-10-29T00:00:00Z"}}}}}}}
	data, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRetainedEvidenceTimeoutCanRecoverButPendingCannotComplete(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	e = requestCancellation(t, f, repo, e)
	if _, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.CancelUnknown, 1); err != nil {
		t.Fatal(err)
	}
	target := recordedAttempt(t, f, repo, e)
	if target.UnknownSince != nil {
		t.Fatal("pure timeout created an evidence gap")
	}
	proof := retainedProof(t, target)
	if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, proof); err != nil {
		t.Fatal(err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, 1); changed || !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("pending completed cancellation: %t %v", changed, err)
	}
	if _, _, err := repo.UpdateAttemptPhase(t.Context(), f.tenant, target.JobName, string(jobrun.Failed), target.JobUID, "20"); !errors.Is(err, store.ErrStaleObservation) {
		t.Fatalf("pending attempt advanced: %v", err)
	}
	// A legacy observer cannot replace retained proof with a Job condition.
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelJobEvidence(e)); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("proof downgraded: %v", err)
	}
	target = recordedAttempt(t, f, repo, e)
	if err := repo.MarkEvidenceReleased(t.Context(), f.tenant, target); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkEvidenceReleased(t.Context(), f.tenant, target); err != nil {
		t.Fatal("release replay:", err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.Canceled, 1); err != nil || !changed {
		t.Fatalf("released proof failed completion: %t %v", changed, err)
	}
	pending, err := repo.PendingEvidenceAfter(t.Context(), f.tenant, 0, 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("released proof remained pending: %d %v", len(pending), err)
	}
}

func TestRetainedEvidenceFirstGapIsDurableWithoutPriorProof(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	e = requestCancellation(t, f, repo, e)
	target := recordedAttempt(t, f, repo, e)
	if err := repo.IsolateAttemptEvidence(t.Context(), f.tenant, target, jobrun.ReasonPodIdentityAmbiguous); err != nil {
		t.Fatal(err)
	}
	got := recordedAttempt(t, f, repo, e)
	if got.UnknownSince == nil || got.Execution.Run.Phase != jobrun.CancelUnknown || !jobrun.EvidenceReleasePending(got.TerminationEvidence) {
		t.Fatalf("first gap was not atomically isolated: %+v", got)
	}
	if jobrun.HasPodTerminationEvidence(got.TerminationEvidence, got.JobUID) {
		t.Fatal("revoked checkpoint is trusted proof")
	}
	if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, retainedProof(t, target)); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("stale narrow sample erased gap: %v", err)
	}
	if err := repo.ClearExecutionUnknown(t.Context(), f.tenant, e, e.Run.OwnerRunUID); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("same Job identity cleared proof gap: %v", err)
	}
	if err := repo.IsolateAttemptEvidence(t.Context(), f.tenant, target, jobrun.ReasonPodIdentityAmbiguous); err != nil {
		t.Fatal(err)
	}
	later := recordedAttempt(t, f, repo, e)
	if later.UnknownSince == nil || !got.UnknownSince.Equal(*later.UnknownSince) {
		t.Fatal("repeated gap reset timestamp")
	}
	pending, err := repo.PendingEvidenceAfter(t.Context(), f.tenant, 0, 100)
	if err != nil || len(pending) != 1 || pending[0].ID != target.ID {
		t.Fatalf("gap lost cleanup obligation: %+v %v", pending, err)
	}
}

func TestLegacyFirstGapRejectsProofWithoutPriorEvidence(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	e = requestCancellation(t, f, repo, e)
	if err := repo.InvalidatePodTerminationEvidence(t.Context(), f.tenant, e); err != nil {
		t.Fatal(err)
	}
	got := requireExecution(t, f, repo, e.Run.ID)
	if got.Run.UnknownSince == nil || got.Run.Phase != jobrun.CancelUnknown {
		t.Fatal("first legacy gap was discarded")
	}
	if err := repo.RecordTerminationEvidence(t.Context(), f.tenant, e, cancelPodEvidence(e)); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("late legacy proof erased gap: %v", err)
	}
}

func TestRetainedEvidenceRefusesAReplacementInventory(t *testing.T) {
	f, repo, e := seedObservedExecution(t)
	target := recordedAttempt(t, f, repo, e)
	proof := retainedProof(t, target)
	if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, proof); err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	if err := json.Unmarshal(proof, &changed); err != nil {
		t.Fatal(err)
	}
	changed["pods"].([]any)[0].(map[string]any)["uid"] = "later-pod"
	other, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordAttemptTerminationEvidence(t.Context(), f.tenant, target, other); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("inventory overwritten: %v", err)
	}
	target.TerminationEvidence = other
	if err := repo.MarkEvidenceReleased(t.Context(), f.tenant, target); !errors.Is(err, store.ErrRunChanged) {
		t.Fatalf("different proof marked done: %v", err)
	}
	if changed, err := repo.UpdateRunPhase(t.Context(), f.tenant, e.Run.ID, jobrun.RetryWaiting, 1); err != nil || changed {
		t.Fatalf("pending released retry: %t %v", changed, err)
	}
}
