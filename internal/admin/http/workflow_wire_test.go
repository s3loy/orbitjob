package http

import (
	"encoding/json"
	"testing"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestWorkflowRootDependenciesAreAnArray(t *testing.T) {
	items := newWorkflowTaskItems(v1alpha1.WorkflowJobSpec{Tasks: []v1alpha1.WorkflowTaskSpec{{Name: "root", JobRef: v1alpha1.WorkflowTaskJobRef{Name: "job"}}}})
	wire, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `[{"name":"root","job_ref":"job","depends_on":[]}]` {
		t.Fatalf("root dependencies must match the OpenAPI array contract: %s", wire)
	}
}
