package operator

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestScheduledJobFromUnstructured(t *testing.T) {
	obj := unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "workloads.orbitjob.io/v1alpha1", "kind": "ScheduledJob", "metadata": map[string]interface{}{"name": "report", "namespace": "finance", "uid": "u-1", "generation": int64(2)}, "spec": map[string]interface{}{"schedule": "0 2 * * *", "jobTemplate": map[string]interface{}{"image": "repo/report:v1"}}}}
	got, err := scheduledJobFromUnstructured(obj)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "report" || got.Spec.Schedule != "0 2 * * *" || got.Spec.JobTemplate.Image != "repo/report:v1" {
		t.Fatalf("unexpected object: %+v", got)
	}
}
