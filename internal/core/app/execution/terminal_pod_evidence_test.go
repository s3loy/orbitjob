package execution

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTerminalPodIncompleteExitEvidenceIsAmbiguous(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{"missing-regular-status", func(p *corev1.Pod) { p.Status.ContainerStatuses = nil }},
		{"waiting-status", func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}
		}},
		{"running-status", func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
		}},
		{"missing-finished-time", func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].State.Terminated.FinishedAt = metav1.Time{}
		}},
		{"missing-init-status", func(p *corev1.Pod) {
			p.Spec.InitContainers = []corev1.Container{{Name: "init"}}
		}},
		{"missing-ephemeral-status", func(p *corev1.Pod) {
			p.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}}}
		}},
	}
	for _, phase := range []corev1.PodPhase{corev1.PodFailed, corev1.PodSucceeded} {
		for _, tc := range cases {
			t.Run(string(phase)+"/"+tc.name, func(t *testing.T) {
				job, pod := heldJobAndPod()
				pod.Status.Phase = phase
				tc.mutate(&pod)
				adapter := Adapter{Client: &terminationInventory{pods: []corev1.Pod{pod}}}
				proof, complete, err := adapter.InspectRetainedTermination(t.Context(), job, nil)
				if !errors.Is(err, ErrPodIdentityAmbiguous) || complete || len(proof) != 0 {
					t.Fatalf("terminal incomplete exit was not isolated: complete=%t err=%v", complete, err)
				}
				// The same unfinished container observation is ordinary waiting
				// while the Pod itself is still nonterminal.
				pod.Status.Phase = corev1.PodPending
				adapter.Client = &terminationInventory{pods: []corev1.Pod{pod}}
				if _, _, err = adapter.InspectRetainedTermination(t.Context(), job, nil); !errors.Is(err, ErrPodWorkloadActive) {
					t.Fatalf("nonterminal Pod did not retain waiting semantics: %v", err)
				}
			})
		}
	}
}
