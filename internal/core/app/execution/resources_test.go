package execution

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"orbitjob/internal/domain/executionpolicy"
)

func TestPinnedResourceRequirementsReachPodAndAreCopied(t *testing.T) {
	declared, err := executionpolicy.Normalize(1, false, 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := ResourceRequirements(declared)
	if err != nil {
		t.Fatal(err)
	}
	job := BuildJob(Identity{}, "finance", Template{Image: "registry.example.com/finance/extract:reviewed", Resources: resources})
	resources.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
	container := job.Spec.Template.Spec.Containers[0]
	want := resource.MustParse("256Mi")
	memory := container.Resources.Limits[corev1.ResourceMemory]
	if memory.Cmp(want) != 0 || len(container.Resources.Requests) != 3 {
		t.Fatalf("rendered Pod did not pin resources: %+v", container.Resources)
	}
	legacy, err := ResourceRequirements(nil)
	if err != nil || len(legacy.Requests) != 0 || len(legacy.Limits) != 0 {
		t.Fatalf("legacy acquired resource defaults: %+v err=%v", legacy, err)
	}
}

func TestRestrictedNetworkLabelBelongsOnlyToExplicitV1Template(t *testing.T) {
	for _, version := range []int32{0, 1} {
		job := BuildJob(Identity{}, "finance", Template{ExecutionPolicyVersion: version, Image: "registry.example.com/finance/extract:reviewed"})
		want := ""
		if version == 1 {
			want = RestrictedNetworkProfile
		}
		if job.Labels[LabelExecutionNetworkProfile] != want || job.Spec.Template.Labels[LabelExecutionNetworkProfile] != want {
			t.Fatalf("version=%d labels=%v podLabels=%v", version, job.Labels, job.Spec.Template.Labels)
		}
		pod := job.Spec.Template.Spec
		if version == 0 {
			if pod.SecurityContext != nil || pod.Containers[0].SecurityContext != nil {
				t.Fatal("legacy template acquired a new security contract")
			}
			continue
		}
		security, task := pod.SecurityContext, pod.Containers[0].SecurityContext
		if security == nil || security.RunAsNonRoot == nil || !*security.RunAsNonRoot || security.RunAsUser == nil || *security.RunAsUser != 65532 || security.SeccompProfile == nil || security.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			t.Fatalf("missing v1 Pod security defaults: %+v", security)
		}
		if task == nil || task.AllowPrivilegeEscalation == nil || *task.AllowPrivilegeEscalation || task.Capabilities == nil || len(task.Capabilities.Drop) != 1 || task.Capabilities.Drop[0] != "ALL" || task.ReadOnlyRootFilesystem != nil {
			t.Fatalf("wrong v1 task security defaults: %+v", task)
		}
	}
}
