package operator

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"orbitjob/internal/core/domain/jobrun"
)

func seedDeletionCheckpoint(t *testing.T, s *retainedRuns, j *retainedJobs, release string) []byte {
	t.Helper()
	j.terminate(t)
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue}}
	proof, complete, err := j.adapter.InspectRetainedTermination(t.Context(), j.job, nil)
	if err != nil || !complete {
		t.Fatal(err)
	}
	s.stored.TerminationEvidence = proof
	s.setEvidence("stop_boundary", "deleting")
	s.setEvidence("hold_release", release)
	return checkpointInventory(t, s.stored.TerminationEvidence)
}

func checkpointInventory(t *testing.T, proof []byte) []byte {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(proof, &body); err != nil {
		t.Fatal(err)
	}
	if string(body["stop_boundary"]) != `"deleting"` || len(body["pods"]) == 0 {
		t.Fatal("isolation rewrote or lost the original deletion checkpoint")
	}
	return body["pods"]
}

func TestCancellationIsolatesOldDeletionCheckpointsWithoutLegacyFallback(t *testing.T) {
	for _, release := range []string{"pending", "done"} {
		for _, presence := range []string{"present", "missing"} {
			t.Run(release+"/"+presence, func(t *testing.T) {
				r, s, j, obj, spec := retainedCancellationFixture(t)
				inventory := seedDeletionCheckpoint(t, s, j, release)
				j.gone = presence == "missing"
				if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
					t.Fatal(err)
				}
				if s.stored.Phase != jobrun.CancelUnknown || s.stored.UnknownSince == nil || s.isolations == 0 ||
					s.proofWrites != 0 || s.releases != 0 || len(j.deleted) != 0 || j.stopRequests != 0 || !j.held(t) {
					t.Fatal("deletion checkpoint escaped retained isolation or authorized a mutation")
				}
				if !jobrun.EvidenceReleasePending(s.stored.TerminationEvidence) || !bytes.Equal(inventory, checkpointInventory(t, s.stored.TerminationEvidence)) {
					t.Fatal("isolation erased original inventory or its cleanup obligation")
				}
				// A subsequent healthy Job view cannot rehabilitate the old sample.
				j.gone = false
				if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
					t.Fatal(err)
				}
				if s.stored.Phase != jobrun.CancelUnknown || s.proofWrites != 0 || s.releases != 0 || !j.held(t) {
					t.Fatal("later acknowledgement erased an earlier evidence gap")
				}
			})
		}
	}
}

func TestHistoricalDeletionDoneRemainsRecoverableAndIsolated(t *testing.T) {
	r, s, j, _, _ := retainedCancellationFixture(t)
	inventory := seedDeletionCheckpoint(t, s, j, "done")
	s.stored.Phase, s.stored.Attempt = jobrun.Succeeded, 2
	s.targetNumber, j.gone = 1, true
	if _, err := r.RecoverExecutions(t.Context(), []string{executionTenant}, 10, &ExecutionRecoveryCursor{}); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Succeeded || s.stored.Attempt != 2 || len(s.phases) != 0 ||
		s.stored.UnknownSince == nil || s.isolations == 0 || s.releases != 0 || !j.held(t) ||
		!jobrun.EvidenceReleasePending(s.stored.TerminationEvidence) || !bytes.Equal(inventory, checkpointInventory(t, s.stored.TerminationEvidence)) {
		t.Fatal("historical invalid done checkpoint escaped isolation or changed current run")
	}
}

func TestDeletingJobWithoutAcknowledgementOnlyExhaustsConfirmationBudget(t *testing.T) {
	for _, elapsed := range []time.Duration{4 * time.Minute, 6 * time.Minute} {
		r, s, j, obj, spec := retainedCancellationFixture(t)
		j.terminate(t)
		at := metav1.Now()
		j.job.DeletionTimestamp = &at
		s.e.PhaseUpdatedAt = r.now().Add(-elapsed)
		if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
			t.Fatal(err)
		}
		want := jobrun.CancelRequested
		if elapsed > 5*time.Minute {
			want = jobrun.CancelUnknown
		}
		if s.stored.Phase != want || s.stored.UnknownSince != nil || s.proofWrites != 0 || s.releases != 0 ||
			len(j.deleted) != 0 || j.stopRequests != 0 || !j.held(t) {
			t.Fatal("missing acknowledgement created proof, a definite gap or an unbounded wait")
		}
	}
}
