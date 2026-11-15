package execution

import (
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"strings"
	"testing"
)

func TestLongJobNamesDoNotShareLegacyEightHexCollision(t *testing.T) {
	// Independently found SHA256 prefix collision for the previous derivation.
	a := Identity{RunName: strings.Repeat("a", 80) + "35442", Attempt: 1}
	b := Identity{RunName: strings.Repeat("a", 80) + "114242", Attempt: 1}
	if LegacyJobName(a) != LegacyJobName(b) {
		t.Fatal("fixture no longer exercises legacy collision")
	}
	if JobName(a) == JobName(b) {
		t.Fatal("different runs collide at Kubernetes workload address")
	}
	for _, id := range []Identity{a, b, {RunName: strings.Repeat("z", 253), Attempt: 2147483647}} {
		name := JobName(id)
		if len(name) > 63 {
			t.Fatalf("job name exceeds controller label limit: %s", name)
		}
		job := BuildJob(id, "tasks", Template{Image: "task"})
		if errs := metav1validation.ValidateLabels(job.Labels, field.NewPath("metadata", "labels")); len(errs) > 0 {
			t.Fatal(errs)
		}
	}
}
