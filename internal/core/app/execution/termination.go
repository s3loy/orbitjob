package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrPodIdentityAmbiguous = errors.New("workload Pod identity is ambiguous")
var ErrPodWorkloadActive = errors.New("workload Pod termination is not established")

// PodInventory is deliberately unfiltered: mutable labels cannot prove absence.
// A failed/expired page invalidates the entire sample, including earlier pages.
func (c KubernetesJobClient) PodInventory(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	if c.Core == nil {
		return nil, fmt.Errorf("core Kubernetes client required")
	}
	var out []corev1.Pod
	next := ""
	for page := 0; page < 100; page++ {
		list, err := c.Core.Pods(namespace).List(ctx, metav1.ListOptions{Limit: 100, Continue: next})
		if err != nil {
			return nil, err
		}
		out = append(out, list.Items...)
		next = list.Continue
		if next == "" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("pod inventory page budget exceeded")
}

type podFact struct {
	UID        string                   `json:"uid"`
	Name       string                   `json:"name"`
	Version    string                   `json:"resource_version"`
	Containers []corev1.ContainerStatus `json:"containers"`
}

func (a Adapter) ownedPodInventory(ctx context.Context, namespace, name, uid string) ([]corev1.Pod, error) {
	observer, ok := a.Client.(interface {
		PodInventory(context.Context, string) ([]corev1.Pod, error)
	})
	if !ok || uid == "" {
		return nil, fmt.Errorf("UID-based Pod inventory required")
	}
	pods, err := observer.PodInventory(ctx, namespace)
	if err != nil {
		return nil, err
	}
	var owned []corev1.Pod
	for _, pod := range pods {
		owner := metav1.GetControllerOf(&pod)
		if owner != nil && owner.Kind == "Job" && owner.APIVersion == "batch/v1" && string(owner.UID) == uid {
			if owner.Name != name || pod.UID == "" {
				return nil, ErrPodIdentityAmbiguous
			}
			owned = append(owned, pod)
		} else if pod.Labels[batchv1.JobNameLabel] == name {
			return nil, ErrPodIdentityAmbiguous
		}
	}
	return owned, nil
}

func terminationFact(pod corev1.Pod) (podFact, error) {
	fact := podFact{UID: string(pod.UID), Name: pod.Name, Version: pod.ResourceVersion}
	if pod.UID == "" || pod.Status.Reason == "NodeLost" {
		return fact, ErrPodIdentityAmbiguous
	}
	fact.Containers = append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...)
	fact.Containers = append(fact.Containers, pod.Status.EphemeralContainerStatuses...)
	for _, status := range fact.Containers {
		if status.State.Terminated != nil && status.State.Terminated.Reason == "ContainerStatusUnknown" {
			return fact, ErrPodIdentityAmbiguous
		}
	}
	if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
		return fact, ErrPodWorkloadActive
	}
	// A terminal phase without complete container exits is a definite evidence
	// gap. PodGC can mark an unscheduled deleting Pod Failed without ever
	// observing its containers; it must not remain an ordinary grace wait.
	terminated := map[string]bool{}
	for _, status := range fact.Containers {
		if status.Name == "" || terminated[status.Name] {
			return fact, ErrPodIdentityAmbiguous
		}
		if status.State.Terminated == nil || status.State.Terminated.FinishedAt.IsZero() {
			return fact, ErrPodIdentityAmbiguous
		}
		terminated[status.Name] = true
	}
	for _, container := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
		if !terminated[container.Name] {
			return fact, ErrPodIdentityAmbiguous
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if !terminated[container.Name] {
			return fact, ErrPodIdentityAmbiguous
		}
	}
	if len(pod.Spec.Containers) == 0 {
		return fact, ErrPodIdentityAmbiguous
	}
	return fact, nil
}

// InspectTermination supports legacy attempts without a Pod retention protocol.
// It requires a nonempty complete inventory and never treats absence as proof.
func (a Adapter) InspectTermination(ctx context.Context, namespace, name, uid string) ([]byte, bool, error) {
	pods, err := a.ownedPodInventory(ctx, namespace, name, uid)
	if err != nil {
		return nil, false, err
	}
	var facts []podFact
	for _, pod := range pods {
		fact, err := terminationFact(pod)
		if err != nil {
			return nil, false, err
		}
		facts = append(facts, fact)
	}
	if len(facts) == 0 {
		return nil, false, nil
	}
	data, err := json.Marshal(struct {
		Source string    `json:"source"`
		JobUID string    `json:"job_uid"`
		At     time.Time `json:"observed_at"`
		Pods   []podFact `json:"pods"`
	}{"pods", uid, time.Now().UTC(), facts})
	return data, err == nil, err
}
