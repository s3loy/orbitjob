package execution

import (
	"encoding/json"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeletionDoesNotAcknowledgeControllerStop(t *testing.T) {
	job, pod := heldJobAndPod()
	at := metav1.Now()
	job.DeletionTimestamp, job.Status.Conditions = &at, nil
	adapter := Adapter{Client: &terminationInventory{pods: []corev1.Pod{pod}}}
	proof, complete, err := adapter.InspectRetainedTermination(t.Context(), job, nil)
	if StopBoundary(job) != "" || err != nil || complete || len(proof) != 0 {
		t.Fatal("deletion-only Job accepted a complete inventory")
	}
	for _, condition := range []batchv1.JobConditionType{batchv1.JobFailureTarget, batchv1.JobSuccessCriteriaMet, batchv1.JobFailed, batchv1.JobComplete} {
		job.Status.Conditions = []batchv1.JobCondition{{Type: condition, Status: corev1.ConditionFalse}}
		if StopBoundary(job) != "" {
			t.Fatal("false controller condition acknowledged a stop")
		}
		job.Status.Conditions[0].Status = corev1.ConditionTrue
		if proof, complete, err = adapter.InspectRetainedTermination(t.Context(), job, nil); err != nil || !complete || len(proof) == 0 {
			t.Fatalf("acknowledgement %s was rejected: %v", condition, err)
		}
	}
}

func TestDeletionOnlyCheckpointCannotBeRepairedOrReleased(t *testing.T) {
	for _, release := range []string{"pending", "done"} {
		t.Run(release, func(t *testing.T) {
			job, pod := heldJobAndPod()
			client := fake.NewClientset(&pod)
			adapter := Adapter{Client: KubernetesJobClient{Client: client.BatchV1(), Core: client.CoreV1()}}
			proof, complete, err := adapter.InspectRetainedTermination(t.Context(), job, nil)
			if err != nil || !complete {
				t.Fatal(err)
			}
			var old retainedProof
			if err = json.Unmarshal(proof, &old); err != nil {
				t.Fatal(err)
			}
			old.StopBoundary, old.HoldRelease = "deleting", release
			proof, err = json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			// A later genuine acknowledgement cannot rewrite the earlier sample.
			if _, complete, err = adapter.InspectRetainedTermination(t.Context(), job, proof); complete || !errors.Is(err, ErrPodIdentityAmbiguous) {
				t.Fatal("old deletion sample became an acknowledged inventory")
			}
			if err = adapter.ReleaseTerminationEvidence(t.Context(), job.Namespace, job.Name, string(job.UID), proof); !errors.Is(err, ErrPodIdentityAmbiguous) {
				t.Fatal("old deletion checkpoint authorized hold release")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" {
					t.Fatal("invalid proof changed a Pod")
				}
			}
		})
	}
}
