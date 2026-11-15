package operator

import (
	"context"
	"encoding/json"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/jobrun"
)

// This controlled observer holds one retry-Pod create outside the inventory.
// A controller acknowledgement is published only after that create completes.
type inflightCreateJobs struct {
	*retainedJobs
	t             *testing.T
	stopRequests  int
	createPending bool
}

func (j *inflightCreateJobs) RequestStop(_ context.Context, job *batchv1.Job) (bool, error) {
	if job.UID != j.job.UID || job.ResourceVersion != j.job.ResourceVersion {
		j.t.Fatal("stop request lost UID/resourceVersion guard")
	}
	if j.job.Spec.ActiveDeadlineSeconds != nil && *j.job.Spec.ActiveDeadlineSeconds <= 1 {
		return false, nil
	}
	deadline := int64(1)
	j.job.Spec.ActiveDeadlineSeconds = &deadline
	j.stopRequests++
	return true, nil
}

func (j *inflightCreateJobs) completeCreate() {
	j.t.Helper()
	pod := j.pod(j.t).DeepCopy()
	pod.Name, pod.UID, pod.ResourceVersion = "late-retry-pod", "late-retry-uid", "later"
	pod.Finalizers = []string{execution.TerminationEvidenceFinalizer}
	pod.Status = corev1.PodStatus{Phase: corev1.PodRunning}
	if _, err := j.client.CoreV1().Pods("finance").Create(j.t.Context(), pod, metav1.CreateOptions{}); err != nil {
		j.t.Fatal(err)
	}
	j.createPending = false
}

func (j *inflightCreateJobs) ReleaseTerminationEvidence(ctx context.Context, namespace, name, uid string, proof []byte) error {
	if err := j.retainedJobs.ReleaseTerminationEvidence(ctx, namespace, name, uid, proof); err != nil {
		return err
	}
	// The previous deletion-based sequence can let GC remove the Job before
	// the in-flight create becomes visible. This callback fixes that ordering
	// deterministically without claiming to run the Kubernetes controller.
	if j.createPending && j.job.DeletionTimestamp != nil {
		j.completeCreate()
	}
	return nil
}

func TestCancellationWaitsForControllerCreateAcknowledgement(t *testing.T) {
	r, s, original, obj, spec := retainedCancellationFixture(t)
	original.terminate(t)
	pod := original.pod(t)
	pod.Status.Phase = corev1.PodFailed
	pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 1
	if _, err := original.client.CoreV1().Pods("finance").UpdateStatus(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	backoff := int32(1)
	original.job.Spec.BackoffLimit = &backoff
	j := &inflightCreateJobs{retainedJobs: original, t: t, createPending: true}
	r.Jobs = j
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelRequested || s.proofWrites != 0 || s.releases != 0 || len(j.deleted) != 0 || j.stopRequests != 1 || !j.held(t) {
		t.Fatal("cancellation sampled or deleted the Job before its outstanding create was acknowledged")
	}
	j.completeCreate()
	j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
	if err := r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.CancelRequested || s.proofWrites != 0 || len(j.deleted) != 0 {
		t.Fatal("controller acknowledgement was mistaken for all containers exiting")
	}
	late, err := j.client.CoreV1().Pods("finance").Get(t.Context(), "late-retry-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	late.Status = j.pod(t).Status
	if _, err = j.client.CoreV1().Pods("finance").UpdateStatus(t.Context(), late, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = r.reconcileCancellation(t.Context(), executionTenant, obj, s.stored, spec); err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Boundary string `json:"stop_boundary"`
		Pods     []struct {
			UID string `json:"uid"`
		} `json:"pods"`
	}
	if err = json.Unmarshal(s.stored.TerminationEvidence, &proof); err != nil {
		t.Fatal(err)
	}
	if s.stored.Phase != jobrun.Canceled || proof.Boundary != "failure_target" || len(proof.Pods) != 2 || len(j.deleted) != 1 || s.proofWrites != 1 {
		t.Fatal("acknowledged complete inventory did not settle the original two-Pod attempt")
	}
}
