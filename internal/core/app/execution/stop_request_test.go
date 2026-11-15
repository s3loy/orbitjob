package execution

import (
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func stopRequestJob() *batchv1.Job {
	job, _ := heldJobAndPod()
	job.Status.Conditions = nil
	return job
}

func TestStopRequestOnlyShortensDeadlineAndRetainsEvidenceTemplate(t *testing.T) {
	for _, initial := range []int64{-1, 0, 1, 3600} {
		job := stopRequestJob()
		if initial >= 0 {
			job.Spec.ActiveDeadlineSeconds = &initial
		}
		client := fake.NewClientset(job)
		adapter := Adapter{Client: KubernetesJobClient{Client: client.BatchV1()}}
		changed, err := adapter.RequestStop(t.Context(), job)
		if err != nil || changed != (initial < 0 || initial > 1) {
			t.Fatalf("initial=%d changed=%t err=%v", initial, changed, err)
		}
		current, err := client.BatchV1().Jobs(job.Namespace).Get(t.Context(), job.Name, metav1.GetOptions{})
		if err != nil || current.UID != job.UID || current.DeletionTimestamp != nil || !RetainsTerminationEvidence(current) {
			t.Fatal("stop request removed original Job or retention protocol")
		}
		want := int64(1)
		if initial == 0 {
			want = 0
		}
		if current.Spec.ActiveDeadlineSeconds == nil || *current.Spec.ActiveDeadlineSeconds != want {
			t.Fatal("stop request extended or omitted deadline")
		}
		before := len(client.Actions())
		if changed, err = adapter.RequestStop(t.Context(), current); err != nil || changed || len(client.Actions()) != before {
			t.Fatal("repeated stop reset the deadline")
		}
		if StopBoundary(current) != "" {
			t.Fatal("patch acceptance was mistaken for controller acknowledgement")
		}
	}
}

func TestStopRequestUIDAndResourceVersionAreAtomic(t *testing.T) {
	for _, field := range []string{"uid", "resource-version"} {
		t.Run(field, func(t *testing.T) {
			observed := stopRequestJob()
			current := observed.DeepCopy()
			if field == "uid" {
				current.UID = "replacement"
			} else {
				current.ResourceVersion = "later"
			}
			client := fake.NewClientset(current)
			adapter := Adapter{Client: KubernetesJobClient{Client: client.BatchV1()}}
			if _, err := adapter.RequestStop(t.Context(), observed); err == nil {
				t.Fatal("stale stop request changed Job spec")
			}
			after, err := client.BatchV1().Jobs(current.Namespace).Get(t.Context(), current.Name, metav1.GetOptions{})
			if err != nil || after.Spec.ActiveDeadlineSeconds != nil || after.UID != current.UID {
				t.Fatal("failed precondition partly changed Job")
			}
		})
	}
}

func TestStopRequestRejectsIncompleteIdentityAndDeletingJob(t *testing.T) {
	for _, mutate := range []func(*batchv1.Job){
		func(j *batchv1.Job) { j.Name = "" },
		func(j *batchv1.Job) { j.Namespace = "" },
		func(j *batchv1.Job) { j.UID = "" },
		func(j *batchv1.Job) { j.ResourceVersion = "" },
		func(j *batchv1.Job) { at := metav1.Now(); j.DeletionTimestamp = &at },
	} {
		job := stopRequestJob()
		mutate(job)
		if _, err := (Adapter{}).RequestStop(t.Context(), job); err == nil {
			t.Fatal("invalid stop identity accepted")
		}
	}
	if _, err := (Adapter{}).RequestStop(t.Context(), nil); err == nil {
		t.Fatal("nil Job accepted")
	}
	client := fake.NewClientset(stopRequestJob())
	failure := errors.New("request timeout")
	client.PrependReactor("patch", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, failure })
	if _, err := (Adapter{Client: KubernetesJobClient{Client: client.BatchV1()}}).RequestStop(t.Context(), stopRequestJob()); !errors.Is(err, failure) {
		t.Fatal("stop transport error lost")
	}
}
