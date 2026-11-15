package projection

import (
	"encoding/json"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestV1ProjectionPinsResourcesAndRetryDefaults(t *testing.T) {
	obj := validScheduledJob()
	obj.Spec.ExecutionPolicyVersion = 1
	w := &captureWriter{}
	if _, err := (Service{Revisions: w}).ApplyScheduledJob(t.Context(), obj, serviceFixtureTenantID, "operator"); err != nil {
		t.Fatal(err)
	}
	var pinned v1alpha1.ScheduledJobSpec
	if err := json.Unmarshal([]byte(w.got.NormalizedSpec), &pinned); err != nil {
		t.Fatal(err)
	}
	if pinned.RetryPolicy.MaxAttempts != 1 || pinned.JobTemplate.BackoffLimit != 0 || pinned.JobTemplate.Resources == nil || pinned.JobTemplate.Resources.Limits["memory"] != "256Mi" {
		t.Fatalf("new revision omitted resolved policy: %s", w.got.NormalizedSpec)
	}
	if obj.Spec.JobTemplate.Resources != nil || obj.Spec.RetryPolicy.MaxAttempts != 0 {
		t.Fatal("projection mutated the caller's source spec")
	}
	obj.Spec.RetryPolicy.MaxAttempts = 2
	if _, err := (Service{Revisions: w}).ApplyScheduledJob(t.Context(), obj, serviceFixtureTenantID, "operator"); err == nil {
		t.Fatal("unsafe retries reached revision writer")
	}
	if w.calls != 1 {
		t.Fatal("invalid policy wrote another revision")
	}
}
