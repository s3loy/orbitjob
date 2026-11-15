package execution

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"orbitjob/internal/core/domain/jobrun"
)

func heldJobAndPod() (*batchv1.Job, corev1.Pod) {
	job := BuildJob(Identity{RunName: "run", RunUID: "run-uid", RevisionID: "7", Attempt: 1}, "tasks", Template{Image: "test.invalid/task"})
	job.Name, job.UID, job.ResourceVersion = "attempt", "original", "23"
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue}}
	pod := terminatedPod(string(job.UID))
	pod.Finalizers = []string{"foreign.example/evidence", TerminationEvidenceFinalizer}
	return job, pod
}

func TestRetainedEvidenceCollectsBeforeFinalJobCondition(t *testing.T) {
	job, pod := heldJobAndPod()
	if !RetainsTerminationEvidence(job) || Phase(*job) == PhaseFailed {
		t.Fatal("FailureTarget changed terminal semantics or creation-time hold missing")
	}
	adapter := Adapter{Client: &terminationInventory{pods: []corev1.Pod{pod}}}
	proof, complete, err := adapter.InspectRetainedTermination(t.Context(), job, nil)
	if err != nil || !complete || !jobrun.HasRetainedTerminationEvidence(proof, string(job.UID)) || !jobrun.EvidenceReleasePending(proof) {
		t.Fatalf("proof=%s complete=%t err=%v", proof, complete, err)
	}
	var saved retainedProof
	if err = json.Unmarshal(proof, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.StopBoundary != "failure_target" || len(saved.Pods) != 1 || saved.Pods[0].UID != string(pod.UID) {
		t.Fatal("proof lost immutable identity or preterminal stop boundary")
	}
}

func TestRetainedEvidenceRefusesUnprotectedInventoryAndWaitsForGrace(t *testing.T) {
	for _, kind := range []string{"empty", "missing-hold", "active", "unknown-status", "NodeLost", "no-boundary"} {
		t.Run(kind, func(t *testing.T) {
			job, pod := heldJobAndPod()
			pods := []corev1.Pod{pod}
			switch kind {
			case "empty":
				pods = nil
			case "missing-hold":
				pods[0].Finalizers = nil
			case "active":
				pods[0].Status.Phase = corev1.PodRunning
			case "unknown-status":
				pods[0].Status.ContainerStatuses[0].State.Terminated.Reason = "ContainerStatusUnknown"
			case "NodeLost":
				pods[0].Status.Reason = "NodeLost"
			case "no-boundary":
				job.Status.Conditions = nil
			}
			proof, complete, err := (Adapter{Client: &terminationInventory{pods: pods}}).InspectRetainedTermination(t.Context(), job, nil)
			if complete || len(proof) != 0 {
				t.Fatal("uncertain inventory became retained proof")
			}
			if kind == "active" && !errors.Is(err, ErrPodWorkloadActive) {
				t.Fatalf("ordinary grace must wait: %v", err)
			}
			if kind != "active" && kind != "no-boundary" && !errors.Is(err, ErrPodIdentityAmbiguous) {
				t.Fatalf("definite evidence gap not isolated: %v", err)
			}
		})
	}
}

func TestRetainedEvidenceSurvivesPartialReleaseWithoutNarrowing(t *testing.T) {
	job, pod := heldJobAndPod()
	other := pod.DeepCopy()
	other.Name, other.UID = "other-pod", "other-uid"
	inventory := &terminationInventory{pods: []corev1.Pod{pod, *other}}
	adapter := Adapter{Client: inventory}
	proof, complete, err := adapter.InspectRetainedTermination(t.Context(), job, nil)
	if err != nil || !complete {
		t.Fatal(err)
	}
	inventory.pods = []corev1.Pod{*other}
	replayed, complete, err := adapter.InspectRetainedTermination(t.Context(), job, proof)
	if err != nil || !complete || string(replayed) != string(proof) {
		t.Fatal("partial release replaced the original complete proof")
	}
	inventory.pods[0].UID = "replacement"
	if _, _, err = adapter.InspectRetainedTermination(t.Context(), job, proof); !errors.Is(err, ErrPodIdentityAmbiguous) {
		t.Fatal("replacement entered original proof")
	}
}

func TestPodHoldReleasePreservesOtherFinalizersAndGuardsIdentity(t *testing.T) {
	for _, change := range []string{"none", "uid", "resource-version"} {
		t.Run(change, func(t *testing.T) {
			_, observed := heldJobAndPod()
			current := observed.DeepCopy()
			if change == "uid" {
				current.UID = "replacement"
			}
			if change == "resource-version" {
				current.ResourceVersion = "later"
			}
			client := fake.NewClientset(current)
			adapter := KubernetesJobClient{Client: client.BatchV1(), Core: client.CoreV1()}
			err := adapter.ReleasePodEvidence(t.Context(), "tasks", observed)
			if (err != nil) != (change != "none") {
				t.Fatalf("unexpected guarded patch result: %v", err)
			}
			after, err := client.CoreV1().Pods("tasks").Get(t.Context(), observed.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(after.Finalizers, "foreign.example/evidence") || slices.Contains(after.Finalizers, TerminationEvidenceFinalizer) != (change != "none") {
				t.Fatalf("unexpected finalizers: %v", after.Finalizers)
			}
		})
	}
}

func TestReleaseRejectsNewOwnedPodAfterProofCommit(t *testing.T) {
	job, pod := heldJobAndPod()
	proof, _, err := (Adapter{Client: &terminationInventory{pods: []corev1.Pod{pod}}}).InspectRetainedTermination(t.Context(), job, nil)
	if err != nil {
		t.Fatal(err)
	}
	additional := pod.DeepCopy()
	additional.Name, additional.UID = "late-pod", "late-uid"
	client := fake.NewClientset(&pod, additional)
	adapter := Adapter{Client: KubernetesJobClient{Client: client.BatchV1(), Core: client.CoreV1()}}
	if err = adapter.ReleaseTerminationEvidence(t.Context(), job.Namespace, job.Name, string(job.UID), proof); !errors.Is(err, ErrPodIdentityAmbiguous) {
		t.Fatalf("late inventory was released: %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "patch" {
			t.Fatal("released a hold after an inventory gap")
		}
	}
}
