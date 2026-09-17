package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestScheduledJobDeepCopyIsolation(t *testing.T) {
	original := &ScheduledJob{Spec: ScheduledJobSpec{JobTemplate: JobTemplateSpec{Args: []string{"original"}}}, Status: ScheduledJobStatus{Conditions: []Condition{{Type: "Ready", Message: "original"}}}}
	copied := original.DeepCopyObject().(*ScheduledJob)
	copied.Spec.JobTemplate.Args[0] = "changed"
	copied.Status.Conditions[0].Message = "changed"
	if original.Spec.JobTemplate.Args[0] != "original" || original.Status.Conditions[0].Message != "original" {
		t.Fatal("deep copy aliases original spec or status")
	}
}

func TestJobRunListDeepCopyIsolation(t *testing.T) {
	original := &JobRunList{Items: []JobRun{{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test": "original"}}, Status: JobRunStatus{KubernetesJobRef: &ObjectReference{Name: "original"}}}}}
	copied := original.DeepCopyObject().(*JobRunList)
	copied.Items[0].Labels["test"] = "changed"
	copied.Items[0].Status.KubernetesJobRef.Name = "changed"
	if original.Items[0].Labels["test"] != "original" || original.Items[0].Status.KubernetesJobRef.Name != "original" {
		t.Fatal("list deep copy aliases original item")
	}
}
