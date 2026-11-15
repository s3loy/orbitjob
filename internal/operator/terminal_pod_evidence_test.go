package operator

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"orbitjob/internal/core/domain/jobrun"
)

func TestUnscheduledFailedPodIsolatesOrdinaryDeadlineExecution(t *testing.T) {
	for _, initial := range []jobrun.Phase{jobrun.CreatingAttempt, jobrun.Running} {
		t.Run(string(initial), func(t *testing.T) {
			r, s, j, _, _ := retainedCancellationFixture(t)
			s.stored.Phase, s.stored.AttemptPhase = initial, initial
			j.job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
			pod := j.pod(t)
			// PodGC can mark an unscheduled deleting Pod Failed without ever
			// observing a container start or writing containerStatuses.
			pod.Spec.NodeName = ""
			pod.Status.Phase, pod.Status.ContainerStatuses = corev1.PodFailed, nil
			if _, err := j.client.CoreV1().Pods("finance").UpdateStatus(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(j.job)
			if err != nil {
				t.Fatal(err)
			}
			if err = r.ReconcileJob(t.Context(), unstructured.Unstructured{Object: raw}); err != nil {
				t.Fatal(err)
			}
			if s.stored.Phase != jobrun.ExecutionUnknown || s.stored.UnknownSince == nil || s.isolations != 1 {
				t.Fatal("terminal incomplete evidence left the run appearing active")
			}
			if !j.held(t) || s.proofWrites != 0 || s.releases != 0 || len(s.phases) != 0 {
				t.Fatal("missing container exit manufactured proof, released the hold or advanced the ledger")
			}
			// A later apparently healthy sample cannot erase the first gap.
			j.terminate(t)
			if err = r.ReconcileJob(t.Context(), unstructured.Unstructured{Object: raw}); err != nil {
				t.Fatal(err)
			}
			if s.stored.Phase != jobrun.ExecutionUnknown || s.proofWrites != 0 || s.releases != 0 || !j.held(t) {
				t.Fatal("later sample silently cleared the evidence gap")
			}
		})
	}
}
