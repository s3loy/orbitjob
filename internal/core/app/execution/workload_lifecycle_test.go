package execution

import (
	"context"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestLongRunIdentityProducesValidLabelsAndRoundTrips(t *testing.T) {
	id := Identity{RunName: strings.Repeat("a", 200), RunUID: string(types.UID("source-uid")), RevisionID: "7", Attempt: 1}
	job := BuildJob(id, "tasks", Template{Image: "task"})
	if errs := metav1validation.ValidateLabels(job.Labels, field.NewPath("metadata", "labels")); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got, err := IdentityFromJob(job); err != nil || got != id {
		t.Fatalf("identity=%+v err=%v", got, err)
	}
	if len(job.OwnerReferences) != 1 || job.OwnerReferences[0].UID != types.UID(id.RunUID) {
		t.Fatal("execution Job is not owned by the run")
	}
}
func TestStopWaitsForPodsEvenAfterJobDisappears(t *testing.T) {
	ctx := context.Background()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "task", Namespace: "tasks", Labels: map[string]string{batchv1.JobNameLabel: "job"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	k := fake.NewClientset(pod)
	adapter := Adapter{Client: KubernetesJobClient{Client: k.BatchV1(), Core: k.CoreV1()}}
	if stopped, err := adapter.Stopped(ctx, "tasks", "job"); err != nil || stopped {
		t.Fatalf("stopped=%v err=%v", stopped, err)
	}
	pod.Status.Phase = corev1.PodSucceeded
	if _, err := k.CoreV1().Pods("tasks").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := adapter.Stopped(ctx, "tasks", "job"); err != nil || !stopped {
		t.Fatalf("stopped=%v err=%v", stopped, err)
	}
}
